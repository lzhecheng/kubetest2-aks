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
	"log"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

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
	// Create a credentials object.
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		log.Fatalf("Authentication failure: %+v", err)
	}

	err = d.deleteResourceGroup(subscriptionId, cred)
	if err != nil {
		log.Fatalf("Creation of resource group failed: %+v", err)
	}

	log.Printf("Resource group deleted")
	return nil
}
