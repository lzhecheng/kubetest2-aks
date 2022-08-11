This is the kubetest2-aks repo to provision and delete aks clusters.

## Commands
Make kubetest2-aks binary
```
make install-deployer
```

Build CCM images with target path or tags
```
kubetest2 aks --build --target cloud-provider-azure --targetPath ../cloud-provider-azure
kubetest2 aks --build --target cloud-provider-azure --targetPath --targetTag v1.24.4
```

Provision an aks cluster in a resource group
```
kubetest2 aks --up --rgName aks-resource-group --location eastus --config cluster-templates/basic-lb.json --customConfig cluster-templates/customconfiguration.json  --clusterName aks-cluster --ccmImageTag abcdefg
```

Delete the resource group
```
kubetest2 aks --down --rgName aks-resource-group --clusterName aks-cluster
```
