/*
Copyright 2022.

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

package controllers

import (
	"context"
	"encoding/json"
	"strings"

	synchronizerv1alpha1 "github.com/a1tan/argocdsecretsynchronizer/api/v1alpha1"
	"github.com/go-logr/logr"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	kops "k8s.io/kops/pkg/kubeconfig"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// SecretSynchronizerReconciler reconciles a SecretSynchronizer object
type SecretSynchronizerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

type Result struct {
	ID   string        `json:"id"`
	Name string        `json:"name"`
	Test []interface{} `json:"test"`
}

type ArgoConfig struct {
	BearerToken     string        `json:"bearerToken"`
	TlsClientConfig ArgoTlsConfig `json:"tlsClientConfig"`
}

type ArgoTlsConfig struct {
	Insecure bool   `json:"insecure"`
	CaData   []byte `json:"caData"`
}

var ManagementClusterPolicyRules = []rbacv1.PolicyRule{
	{
		APIGroups: []string{"*"},
		Resources: []string{"*"},
		Verbs:     []string{"*"},
	},
	{
		NonResourceURLs: []string{"*"},
		Verbs:           []string{"*"},
	},
}
var (
	setupLog = ctrl.Log.WithName("setup")
)

//+kubebuilder:rbac:groups=synchronizer.a1tan,resources=secretsynchronizers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch
//+kubebuilder:rbac:groups=synchronizer.a1tan,resources=secretsynchronizers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=synchronizer.a1tan,resources=secretsynchronizers/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the SecretSynchronizer object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *SecretSynchronizerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// log := r.Log.WithValues("SecretSynchronizer", req.NamespacedName)
	log := ctrllog.FromContext(ctx)
	log.Info("Reconcile method has started")
	// synchronizer := &synchronizerv1alpha1.SecretSynchronizer{}
	// err := r.Get(ctx, req.NamespacedName, synchronizer)
	// if err != nil {
	// 	if errors.IsNotFound(err) {
	// 		// Request object not found, could have been deleted after reconcile request.
	// 		// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
	// 		// Return and don't requeue
	// 		log.Info("Synchronizer resource not found. Ignoring since object must be deleted")
	// 		return ctrl.Result{}, nil
	// 	}
	// 	// Error reading the object - requeue the request.
	// 	log.Error(err, "Failed to get Synchronizer")
	// 	return ctrl.Result{}, err
	// }
	secret := &corev1.Secret{}
	err := r.Get(ctx, req.NamespacedName, secret)
	if err != nil {
		log.Error(err, "Failed to find secret and started to delete related resources")
		return r.DeleteIrrelevantSecrets(ctx)
	}

	log.Info("Secret", "type", secret.Type, "name", secret.Name)
	// var kubeconfig string
	var kubeconfigByte []byte
	var configExists bool
	var kubeconf kops.KubectlConfig

	kubeconfigByte, configExists = secret.Data["kubeconfig"]
	if !configExists {
		kubeconfigByte, configExists = secret.Data["config"]
	}
	if configExists {
		if err != nil {
			log.Error(err, "Kubeconfig Decoding Error")
			return ctrl.Result{}, err
		}

		err = yaml.Unmarshal(kubeconfigByte, &kubeconf)
		if err != nil {
			log.Error(err, "Kubeconfig Json Convert Error")
			return ctrl.Result{}, err
		}

		config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigByte)
		if err != nil {
			log.Error(err, "RestConfig Generation Error")
			return ctrl.Result{}, err
		}

		clientset, err := kubernetes.NewForConfig(config)
		if err != nil {
			log.Error(err, "Clientset Generation Error")
			return ctrl.Result{}, err
		}

		argoSecret, err := CreateServiceAccountWithToken(ctx, clientset, "kube-system", "argocd-manager")
		if err != nil {
			log.Error(err, "Service account creation error")
			return ctrl.Result{}, err
		}
		err = r.CreateArgoCDSecret(ctx, argoSecret, secret, kubeconf)
		if err != nil {
			log.Error(err, "Argo CD secret creation error")
			return ctrl.Result{}, err
		}
		log.Info("Argo CD Secret Created Successfully")
	} else {
		log.Info("Kubeconfig not found", "data", kubeconfigByte)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SecretSynchronizerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&synchronizerv1alpha1.SecretSynchronizer{}).
		Owns(&corev1.Secret{}).
		WithEventFilter(ignoreIrrelevantSecrets()).
		Watches(&source.Kind{Type: &corev1.Secret{}},
			handler.EnqueueRequestsFromMapFunc(
				func(obj client.Object) []reconcile.Request {
					secret, ok := obj.(*corev1.Secret)
					if !ok {
						setupLog.Info("Unable to get secret", "secret", secret)
						return nil
					}
					_, isArgoSecret := secret.Labels["argocd.argoproj.io/secret-type"]
					if isArgoSecret {
						setupLog.Info("Secret already is an ArgoCD secret")
						return nil
					}
					_, configExists := secret.Data["config"]
					_, kubeconfigExists := secret.Data["kubeconfig"]
					if configExists || kubeconfigExists {
						return []reconcile.Request{
							{NamespacedName: types.NamespacedName{
								Name:      secret.GetName(),
								Namespace: secret.GetNamespace(),
							}},
						}
					}
					return nil
				},
			),
		).
		Complete(r)
}

func ignoreIrrelevantSecrets() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			secret, ok := e.ObjectNew.(*corev1.Secret)
			if !ok {
				setupLog.Info("Unable to get secret", "secret", secret, "object", e.ObjectNew)
				return false
			}
			_, configExists := secret.Data["config"]
			_, kubeconfigExists := secret.Data["kubeconfig"]
			return configExists || kubeconfigExists
		},
		CreateFunc: func(e event.CreateEvent) bool {
			secret, ok := e.Object.(*corev1.Secret)
			if !ok {
				setupLog.Info("Unable to get secret", "secret", secret, "object", e.Object)
				return false
			}
			_, configExists := secret.Data["config"]
			_, kubeconfigExists := secret.Data["kubeconfig"]
			return configExists || kubeconfigExists
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}
}

func (r *SecretSynchronizerReconciler) CreateArgoCDSecret(ctx context.Context, argoSecret *corev1.Secret, secret *corev1.Secret, kubeconf kops.KubectlConfig) error {
	var argoTlsConfig ArgoTlsConfig
	argoTlsConfig.Insecure = false
	argoTlsConfig.CaData = argoSecret.Data["ca.crt"]

	var argoConfig ArgoConfig
	argoConfig.BearerToken = string(argoSecret.Data["token"])
	argoConfig.TlsClientConfig = argoTlsConfig

	argoConfigAsJson, _ := json.Marshal(argoConfig)

	err := r.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret.Name + "-argocd",
			Namespace: "argocd",
			Labels: map[string]string{
				"argocd.argoproj.io/secret-type": "cluster",
				"argocdsecretsynchronizer":       secret.Name,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"name":   []byte(kubeconf.Clusters[0].Name),
			"server": []byte(kubeconf.Clusters[0].Cluster.Server),
			"config": argoConfigAsJson,
		},
	})
	return err
}

func (r *SecretSynchronizerReconciler) DeleteIrrelevantSecrets(ctx context.Context) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	secretList := &corev1.SecretList{}
	opts := []client.ListOption{
		client.HasLabels{"argocdsecretsynchronizer"},
	}

	err := r.List(ctx, secretList, opts...)
	if err != nil {
		log.Error(err, "There is an error while finding secrets and exiting reconcile")
		return reconcile.Result{}, nil
	}
	for _, oRef := range secretList.Items {
		mainSecret := &corev1.Secret{}
		err := r.Get(ctx, types.NamespacedName{Name: oRef.Labels["argocdsecretsynchronizer"]}, mainSecret)
		if err != nil {
			//Double check before deleting
			_, isSecretSynchronizerSecret := oRef.Labels["argocdsecretsynchronizer"]
			if isSecretSynchronizerSecret {
				err = r.Delete(ctx, &oRef)
				if err != nil {
					log.Error(err, "Cannot delete related secrets and exiting reconcile")
					return reconcile.Result{}, nil
				}
			}
		}
	}
	return reconcile.Result{}, nil
}
func CreateServiceAccountWithToken(ctx context.Context, clientset kubernetes.Interface, namespace string, name string) (*corev1.Secret, error) {
	log := ctrllog.FromContext(ctx)
	var err error
	clusterRole := rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name + "role",
		},
		Rules: ManagementClusterPolicyRules,
	}
	_, err = clientset.RbacV1().ClusterRoles().Create(context.Background(), &clusterRole, metav1.CreateOptions{})
	if err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			return nil, err
		}
	}

	roleBinding := rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name + "rolebinding",
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     name + "role",
		},
		Subjects: []rbacv1.Subject{rbacv1.Subject{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      name,
			Namespace: namespace,
		}},
	}

	_, err = clientset.RbacV1().ClusterRoleBindings().Create(context.Background(), &roleBinding, metav1.CreateOptions{})
	if err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			return nil, err
		}
	}

	serviceAccount := corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	_, err = clientset.CoreV1().ServiceAccounts(namespace).Create(context.Background(), &serviceAccount, metav1.CreateOptions{})
	if err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			return nil, err
		}
	}

	serviceAccountCreated, _ := clientset.CoreV1().ServiceAccounts(namespace).Get(context.Background(), name, metav1.GetOptions{})
	log.Info("Service Account Created", "Service Account", serviceAccountCreated)

	if serviceAccountCreated.Secrets == nil {
		secretToCreate := corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Secret",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Namespace:   namespace,
				Annotations: map[string]string{"kubernetes.io/service-account.name": serviceAccountCreated.Name},
			},
			Type: corev1.SecretTypeServiceAccountToken,
		}

		secret, err := clientset.CoreV1().Secrets(namespace).Create(context.Background(), &secretToCreate, metav1.CreateOptions{})

		if err != nil {
			log.Error(err, "An error occurred while creating secret")
			return nil, err
		}
		return secret, nil
	}

	for _, oRef := range serviceAccountCreated.Secrets {
		secret, err := clientset.CoreV1().Secrets(namespace).Get(context.Background(), oRef.Name, metav1.GetOptions{})
		if err != nil {
			log.Error(err, "An error occurred while getting secret")
			return nil, err
		}
		if secret.Type == corev1.SecretTypeServiceAccountToken {
			return secret, nil
		}
	}

	return nil, nil
}
