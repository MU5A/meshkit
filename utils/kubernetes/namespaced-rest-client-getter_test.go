package kubernetes

import (
	"testing"

	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func TestNamespacedRESTClientGetterPinsNamespace(t *testing.T) {
	flags := genericclioptions.NewConfigFlags(false)
	ctxNamespace := "from-context"
	flags.Namespace = &ctxNamespace

	base, _, err := flags.ToRawKubeConfigLoader().Namespace()
	if err != nil || base != ctxNamespace {
		t.Fatalf("test setup: got (%q, %v), want %q", base, err, ctxNamespace)
	}

	getter := namespacedRESTClientGetter{RESTClientGetter: flags, namespace: "operators"}
	got, overridden, err := getter.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		t.Fatal(err)
	}
	if got != "operators" || !overridden {
		t.Fatalf("Namespace() = (%q, %v), want (\"operators\", true)", got, overridden)
	}
}
