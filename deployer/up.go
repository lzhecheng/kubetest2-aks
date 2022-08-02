/*
Copyright 2022 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package deployer

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	armcontainerservicev2 "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/go-autorest/autorest/to"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"

	"sigs.k8s.io/kubetest2/pkg/exec"
)

var (
	apiVersion           = "2022-04-02-preview"
	k8sVersion           = "1.24.0"
	defaultKubeconfigDir = "_output/kubetest2-aks/kubeconfig"

	cloudProviderRepoPath = os.Getenv("CLOUD_PROVIDER_REPO_PATH")
	imageRegistry         = os.Getenv("IMAGE_REGISTRY")
)

func runCmd(cmd exec.Cmd) error {
	// exec.NoOutput(cmd)
	exec.InheritOutput(cmd)
	return cmd.Run()
}

// makeImages makes CCM and CNM images.
// NOTICE: docker login is needed first.
func (d *deployer) makeImages() (string, error) {
	// Show commit
	if err := runCmd(exec.Command("git", "show", "--stat")); err != nil {
		return "", fmt.Errorf("failed to show commit: %v", err)
	}

	// Make images
	// TODO: check if images already existing
	targets := []string{"build-ccm-image-amd64", "push-ccm-image-amd64", "build-node-image-linux-amd64", "push-node-image-linux-amd64"}
	for _, target := range targets {
		if err := runCmd(exec.Command("make", target, "-C", cloudProviderRepoPath)); err != nil {
			return "", fmt.Errorf("failed to make %s: %v", target, err)
		}
	}

	imageTag, err := exec.Output(exec.Command("git", "rev-parse", "--short=7", "HEAD"))
	if err != nil {
		return "", fmt.Errorf("failed to get image tag: %v", err)
	}

	return string(imageTag), nil
}

// Define the function to create a resource group.
func (d *deployer) createResourceGroup(subscriptionId string, credential azcore.TokenCredential) (armresources.ResourceGroupsClientCreateOrUpdateResponse, error) {
	rgClient, _ := armresources.NewResourceGroupsClient(subscriptionId, credential, nil)

	param := armresources.ResourceGroup{
		Location: to.StringPtr(location),
	}

	return rgClient.CreateOrUpdate(ctx, resourceGroupName, param, nil)
}

// createAKSWithCustomConfig creates an AKS cluster with custom configuration.
func (d *deployer) createAKSWithCustomConfig(token string, imageTag string) error {
	clusterID := fmt.Sprintf("/subscriptions/%s/resourcegroups/%s/providers/Microsoft.ContainerService/managedClusters/%s", subscriptionId, resourceGroupName, clusterName)
	url := fmt.Sprintf("https://management.azure.com%s?api-version=%s", clusterID, apiVersion)

	basicLBFilePath := "cluster-templates/basic-lb.json"
	basicLBFile, err := ioutil.ReadFile(basicLBFilePath)
	if err != nil {
		return fmt.Errorf("failed to read cluster config file at %q: %v", basicLBFilePath, err)
	}
	clusterConfig := string(basicLBFile)
	replacing := map[string]string{
		"AKS_CLUSTER_ID":      clusterID,
		"CLUSTER_NAME":        clusterName,
		"AZURE_LOCATION":      location,
		"AZURE_CLIENT_ID":     clientID,
		"AZURE_CLIENT_SECRET": clientSecret,
		"KUBERNETES_VERSION":  k8sVersion,
	}
	for k, v := range replacing {
		clusterConfig = strings.ReplaceAll(clusterConfig, k, v)
	}

	customConfigFilePath := "cluster-templates/customconfiguration.json"
	customConfigFile, err := ioutil.ReadFile(customConfigFilePath)
	if err != nil {
		return fmt.Errorf("failed to read custom config file at %q: %v", customConfigFilePath, err)
	}

	imageMap := map[string]string{
		"CUSTOM_CCM_IMAGE": fmt.Sprintf("%s/azure-cloud-controller-manager:%s", imageRegistry, imageTag),
		"CUSTOM_CNM_IMAGE": fmt.Sprintf("%s/azure-cloud-node-manager:%s-linux-amd64", imageRegistry, imageTag),
	}
	customConfig := string(customConfigFile)
	for k, v := range imageMap {
		customConfig = strings.ReplaceAll(customConfig, k, v)
	}

	encodedCustomConfig := base64.StdEncoding.EncodeToString([]byte(customConfig))
	clusterConfig = strings.ReplaceAll(clusterConfig, "CUSTOM_CONFIG", encodedCustomConfig)

	r, err := http.NewRequest("PUT", url, strings.NewReader(clusterConfig))
	if err != nil {
		return fmt.Errorf("failed to generate new PUT request: %v", err)
	}

	// request headers
	r.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))
	r.Header.Add("Content-Type", "application/json")
	r.Header.Add("AKSHTTPCustomFeatures", "Microsoft.ContainerService/EnableCloudControllerManager")

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	resp, err := client.Do(r)
	if err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("failed to create the AKS cluster: output %v\nerr %v", resp, err)
	}
	klog.Infof("An AKS cluster %q in resource group %q is created", clusterName, resourceGroupName)
	return nil
}

// getAKSKubeconfig gets kubeconfig of the AKS cluster and writes it to specific path.
func (d *deployer) getAKSKubeconfig(cred *azidentity.DefaultAzureCredential) error {
	client, err := armcontainerservicev2.NewManagedClustersClient(subscriptionId, cred, nil)
	if err != nil {
		return fmt.Errorf("failed to new managed cluster client with sub ID %q: %v", subscriptionId, err)
	}

	var resp armcontainerservicev2.ManagedClustersClientListClusterUserCredentialsResponse
	err = wait.PollImmediate(10*time.Second, 3*time.Minute, func() (done bool, err error) {
		resp, err = client.ListClusterUserCredentials(ctx, resourceGroupName, clusterName, nil)
		if err != nil {
			if strings.Contains(err.Error(), "404 Not Found") {
				klog.Infof("failed to list cluster user credentials for 10 second, retrying")
				return false, nil
			}
			return false, fmt.Errorf("failed to list cluster user credentials with resource group name %q, cluster ID %q: %v", resourceGroupName, clusterName, err)
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	kubeconfigs := resp.CredentialResults.Kubeconfigs
	if len(kubeconfigs) == 0 {
		return fmt.Errorf("failed to find a valid kubeconfig")
	}
	kubeconfig := kubeconfigs[0]
	destPath := fmt.Sprintf("%s/%s_%s.kubeconfig", defaultKubeconfigDir, resourceGroupName, clusterName)

	if err := os.MkdirAll(defaultKubeconfigDir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to mkdir the default kubeconfig dir: %v", err)
	}
	if err := ioutil.WriteFile(destPath, kubeconfig.Value, 0666); err != nil {
		return fmt.Errorf("failed to write kubeconfig to %s", destPath)
	}

	klog.Infof("Succeeded in getting kubeconfig of cluster %q in resource group %q", clusterName, resourceGroupName)
	return nil
}

func (d *deployer) Up() error {
	// Create a credentials object.
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		klog.Fatalf("Authentication failure: %+v", err)
	}

	// Create the resource group
	resourceGroup, err := d.createResourceGroup(subscriptionId, cred)
	if err != nil {
		return fmt.Errorf("failed to create the resource group: %v", err)
	}
	klog.Infof("Resource group %s created", *resourceGroup.ResourceGroup.ID)

	token, err := cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{"https://management.azure.com/.default"}})
	if err != nil {
		return fmt.Errorf("failed to get token from credential: %v", err)
	}

	// Make CCM and CNM images
	imageTag, err := d.makeImages()
	if err != nil {
		return fmt.Errorf("failed to create CCM and CNM images: %v", err)
	}

	// Create the AKS cluster
	if err := d.createAKSWithCustomConfig(token.Token, imageTag); err != nil {
		return fmt.Errorf("failed to create the AKS cluster: %v", err)
	}

	// Get the cluster kubeconfig
	if err := d.getAKSKubeconfig(cred); err != nil {
		return fmt.Errorf("failed to get AKS cluster kubeconfig: %v", err)
	}
	return nil
}

func (d *deployer) IsUp() (up bool, err error) {
	return false, nil
}
