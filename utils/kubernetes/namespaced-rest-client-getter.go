package kubernetes

import (
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// namespacedRESTClientGetter pins the namespace Helm's kube client works in.
//
// Helm creates objects that carry no namespace of their own in the namespace its kube client
// reports, which comes from the RESTClientGetter's kubeconfig loader - the kubeconfig
// context's namespace, "default" when it sets none. The namespace given to
// action.Configuration.Init only decides where release records are stored. Without pinning,
// a chart installed into namespace X keeps its release record there while its namespaced
// objects land in the context's namespace, and anything that refers to the release namespace
// (a RoleBinding subject, for example) points at the wrong place.
type namespacedRESTClientGetter struct {
	genericclioptions.RESTClientGetter
	namespace string
}

func (g namespacedRESTClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return namespacedClientConfig{delegate: g.RESTClientGetter.ToRawKubeConfigLoader(), namespace: g.namespace}
}

// namespacedClientConfig answers Namespace() with a fixed namespace and delegates the rest.
type namespacedClientConfig struct {
	delegate  clientcmd.ClientConfig
	namespace string
}

func (c namespacedClientConfig) RawConfig() (clientcmdapi.Config, error) {
	return c.delegate.RawConfig()
}

func (c namespacedClientConfig) ClientConfig() (*rest.Config, error) {
	return c.delegate.ClientConfig()
}

func (c namespacedClientConfig) Namespace() (string, bool, error) {
	return c.namespace, true, nil
}

func (c namespacedClientConfig) ConfigAccess() clientcmd.ConfigAccess {
	return c.delegate.ConfigAccess()
}
