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
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/go-autorest/autorest/to"
	"k8s.io/klog"
)

var (
	apiVersion = "2022-04-02-preview"
)

// Define the function to create a resource group.
func (d *deployer) createResourceGroup(subscriptionId string, credential azcore.TokenCredential) (armresources.ResourceGroupsClientCreateOrUpdateResponse, error) {
	rgClient, _ := armresources.NewResourceGroupsClient(subscriptionId, credential, nil)

	param := armresources.ResourceGroup{
		Location: to.StringPtr(location),
	}

	return rgClient.CreateOrUpdate(ctx, resourceGroupName, param, nil)
}

func (d *deployer) createAKSWithCustomConfig(token string) error {
	clusterID := fmt.Sprintf("/subscriptions/%s/resourcegroups/%s/providers/Microsoft.ContainerService/managedClusters/%s", subscriptionId, resourceGroupName, clusterName)
	url := fmt.Sprintf("https://management.azure.com%s?api-version=%s", clusterID, apiVersion)

	basicLBFilePath := "cluster-templates/basic-lb.json"
	basicLBFile, err := ioutil.ReadFile(basicLBFilePath)
	if err != nil {
		return err
	}
	clusterConfig := string(basicLBFile)
	replacing := map[string]string{
		"AKS_CLUSTER_ID":      clusterID,
		"CLUSTER_NAME":        clusterName,
		"AZURE_LOCATION":      location,
		"AZURE_CLIENT_ID":     clientID,
		"AZURE_CLIENT_SECRET": clientSecret,
	}
	for k, v := range replacing {
		clusterConfig = strings.ReplaceAll(clusterConfig, k, v)
	}

	customConfigFilePath := "cluster-templates/customconfiguration.json"
	customConfigFile, err := ioutil.ReadFile(customConfigFilePath)
	customConfig := string(customConfigFile)
	if err != nil {
		return err
	}
	imageMap := map[string]string{
		"CUSTOM_CCM_IMAGE": ccmImage,
		"CUSTOM_CNM_IMAGE": cnmImage,
	}
	for k, v := range imageMap {
		customConfig = strings.ReplaceAll(customConfig, k, v)
	}
	clusterConfig = strings.ReplaceAll(clusterConfig, "CUSTOM_CONFIG", customConfig)

	r, err := http.NewRequest("POST", url, strings.NewReader(clusterConfig))
	if err != nil {
		return err
	}

	// request headers
	r.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))
	r.Header.Add("Content-Type", "application/json")
	r.Header.Add("AKSHTTPCustomFeatures", "Microsoft.ContainerService/EnableCloudControllerManager")

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	klog.Infof("request: %v", r)
	resp, err := client.Do(r)
	klog.Infof("response: %v", resp)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	klog.Infof("AKS cluster is created")

	return nil
}

func (d *deployer) Up() error {
	// Create a credentials object.
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		klog.Fatalf("Authentication failure: %+v", err)
	}

	resourceGroup, err := d.createResourceGroup(subscriptionId, cred)
	if err != nil {
		return fmt.Errorf("creation of resource group failed: %+v", err)
	}
	klog.Infof("Resource group %s created", *resourceGroup.ResourceGroup.ID)

	token, err := cred.GetToken(ctx, policy.TokenRequestOptions{[]string{"https://management.azure.com/.default"}})
	if err != nil {
		return err
	}
	err = d.createAKSWithCustomConfig(token.Token)
	if err != nil {
		return fmt.Errorf("creation of AKS cluster failed: %+v", err)
	}

	return nil
}

func (d *deployer) IsUp() (up bool, err error) {
	return false, nil
}
