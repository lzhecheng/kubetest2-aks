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
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/Azure/go-autorest/autorest/to"
	"github.com/octago/sflags/gen/gpflag"
	"github.com/spf13/pflag"
	"k8s.io/klog"
	"sigs.k8s.io/kubetest2/pkg/types"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

// Name is the name of the deployer
const Name = "aks"

var (
	GitTag string

	subscriptionId    = os.Getenv("AZURE_SUBSCRIPTION_ID")
	location          = os.Getenv("AZURE_LOCATION")
	resourceGroupName = os.Getenv("AZURE_RESOURCEGROUP")
	ctx               = context.Background()
)

type deployer struct {
	// generic parts
	commonOptions types.Options
	// aks specific details
	ClusterName string `flag:"cluster-name" desc:"the aks cluster --name"`
	// BuildType      string `desc:"--type for aks build node-image"`
	ConfigPath     string `flag:"config" desc:"--config for aks create cluster"`
	KubeconfigPath string `flag:"kubeconfig" desc:"--kubeconfig flag for aks create cluster"`
	// KubeRoot       string `desc:"--kube-root for aks build node-image"`

	// logsDir string
}

// New implements deployer.New for aks
func New(opts types.Options) (types.Deployer, *pflag.FlagSet) {
	// create a deployer object and set fields that are not flag controlled
	d := &deployer{
		commonOptions: opts,
		// logsDir:       filepath.Join(opts.RunDir(), "logs"),
	}
	// register flags and return
	return d, bindFlags(d)
}

// Define the function to create a resource group.
func (d *deployer) createResourceGroup(subscriptionId string, credential azcore.TokenCredential) (armresources.ResourceGroupsClientCreateOrUpdateResponse, error) {
	rgClient, _ := armresources.NewResourceGroupsClient(subscriptionId, credential, nil)

	param := armresources.ResourceGroup{
		Location: to.StringPtr(location),
	}

	return rgClient.CreateOrUpdate(ctx, resourceGroupName, param, nil)
}

func (d *deployer) Up() error {
	// Create a credentials object.
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		log.Fatalf("Authentication failure: %+v", err)
	}

	resourceGroup, err := d.createResourceGroup(subscriptionId, cred)
	if err != nil {
		log.Fatalf("Creation of resource group failed: %+v", err)
	}

	log.Printf("Resource group %s created", *resourceGroup.ResourceGroup.ID)
	return nil
}

func (d *deployer) deleteResourceGroup(subscriptionId string, credential azcore.TokenCredential) error {
	rgClient, _ := armresources.NewResourceGroupsClient(subscriptionId, credential, nil)

	poller, err := rgClient.BeginDelete(ctx, resourceGroupName, nil)
	if err != nil {
		return err
	}
	if _, err := poller.PollUntilDone(ctx, nil); err != nil {
		return err
	}
	return nil
}

func (d *deployer) Down() error {
	return nil
}

func (d *deployer) IsUp() (up bool, err error) {
	return false, nil
}

func (d *deployer) DumpClusterLogs() error {
	return nil
}

func (d *deployer) Kubeconfig() (string, error) {
	if d.KubeconfigPath != "" {
		return d.KubeconfigPath, nil
	}
	if kconfig, ok := os.LookupEnv("KUBECONFIG"); ok {
		return kconfig, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".kube", "config"), nil
}

func (d *deployer) Version() string {
	return GitTag
}

// bindFlags is a helper used to create & bind a flagset to the deployer
func bindFlags(d *deployer) *pflag.FlagSet {
	flags, err := gpflag.Parse(d)
	if err != nil {
		klog.Fatalf("unable to generate flags from deployer")
		return nil
	}

	klog.InitFlags(nil)
	flags.AddGoFlagSet(flag.CommandLine)

	return flags
}

// assert that deployer implements types.DeployerWithKubeconfig
var _ types.DeployerWithKubeconfig = &deployer{}
