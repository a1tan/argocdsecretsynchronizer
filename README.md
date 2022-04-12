# Argo CD Secret Synchronizer
This tool is an experimental operator to synchronize secrets between declarative kubernetes cluster managers and Argo CD. There is a difference between secrets which are produced by these cluster managers and Argo CD uses as cluster declaration. 

This operator does a simple thing: 
* Takes the secrets produced when a cluster added
* Uses the kubeconfig on that secret to create service account, role and rolebinding on created cluster 
* Takes the token of the service account created
* Creates the related Argo CD Cluster definition which is also another secret

These cluster managers are:
* Crossplane
* VCluster
* Cluster API(Not tested yet)
* Azure Service Operator(Not tested yet)

## Installation
* Clone this repository
* Run below commands in order

```
kubectl apply -k config/default/
kubectl apply -k config/samples/
```

## Tips
### Vcluster
When using with Vcluster, Vcluster uses localhost as the kubeconfig server which cannot be used to connect created cluster.
To solve this problem below two parameters has to be set. These parameters point to the service created on the Vcluster namespace on parent cluster.

```
syncer:
  extraArgs: 
    - out-kube-config-server: https://{{service}}.{{namespace}}.svc.cluster.local
    - tls-san: {{service}}.{{namespace}}.svc.cluster.local
```