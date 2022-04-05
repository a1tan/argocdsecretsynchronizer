
sudo cp -r /mnt/c/applinux/go /usr/local/go
export KUBECONFIG="/mnt/c/Users/alaltund/.kube/config"
export PATH="$PATH:/mnt/c/applinux"
export GOROOT="/usr/local/go"
# export GOPATH="/mnt/c/Users/alaltund/Desktop/Work/Kubernetes/Operators/ArgoCDSecretSynchronizer"
export PATH="$PATH:$GOROOT/bin"
export GO111MODULE=on