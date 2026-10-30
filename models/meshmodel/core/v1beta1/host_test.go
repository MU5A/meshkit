package v1beta1

import (
	"errors"
	"testing"

	"github.com/meshery/meshkit/utils/kubernetes"
	modelv1beta1 "github.com/meshery/schemas/models/v1beta1/model"
	"github.com/meshery/schemas/models/v1beta3/component"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const chartURI = "https://example.com/charts/some-operator-1.2.3.tgz"

func compWithSource(componentURI, modelURI string) component.ComponentDefinition {
	comp := component.ComponentDefinition{DisplayName: "c"}
	if componentURI != "" {
		comp.Metadata.AdditionalProperties = map[string]interface{}{"source_uri": componentURI}
	}
	if modelURI != "" {
		comp.Model = &modelv1beta1.ModelDefinition{
			Metadata: &modelv1beta1.ModelDefinition_Metadata{
				AdditionalProperties: map[string]interface{}{"source_uri": modelURI},
			},
		}
	}
	return comp
}

func TestDependentChartURI(t *testing.T) {
	tests := []struct {
		name string
		comp component.ComponentDefinition
		want string
	}{
		{"component metadata, where the registry keeps it", compWithSource(chartURI, ""), chartURI},
		{"model metadata only", compWithSource("", chartURI), chartURI},
		{"component wins over model", compWithSource(chartURI, "https://example.com/other-0.1.0.tgz"), chartURI},
		{"tar.gz chart archive", compWithSource("https://example.com/c-1.0.0.tar.gz", ""), "https://example.com/c-1.0.0.tar.gz"},
		{"archive with a query string", compWithSource("https://example.com/c-1.0.0.tgz?token=x", ""), "https://example.com/c-1.0.0.tgz?token=x"},
		{"git repository is not a chart", compWithSource("git://github.com/aws-controllers-k8s/memorydb-controller/main/helm", ""), ""},
		{"raw manifest is not a chart", compWithSource("https://github.com/Azure/azure-service-operator/releases/download/v2.14.0/crds.yaml", ""), ""},
		{"non chart component falls back to a chart on the model", compWithSource("git://github.com/x/y/main/helm", chartURI), chartURI},
		{"no source_uri", compWithSource("", ""), ""},
		{"non-string source_uri", func() component.ComponentDefinition {
			c := component.ComponentDefinition{}
			c.Metadata.AdditionalProperties = map[string]interface{}{"source_uri": 42}
			return c
		}(), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dependentChartURI(tt.comp); got != tt.want {
				t.Fatalf("dependentChartURI() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDependentNamespace(t *testing.T) {
	tests := []struct {
		name string
		cfg  map[string]interface{}
		want string
	}{
		{"top level namespace", map[string]interface{}{"namespace": "ops"}, "ops"},
		{"metadata namespace, the shape designs use", map[string]interface{}{"metadata": map[string]interface{}{"name": "n", "namespace": "meshery"}}, "meshery"},
		{"top level wins", map[string]interface{}{"namespace": "a", "metadata": map[string]interface{}{"namespace": "b"}}, "a"},
		{"neither", map[string]interface{}{"spec": map[string]interface{}{}}, ""},
		{"nil configuration", nil, ""},
		{"namespace of the wrong type", map[string]interface{}{"namespace": 7}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dependentNamespace(component.ComponentDefinition{Configuration: tt.cfg}); got != tt.want {
				t.Fatalf("dependentNamespace() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResourceListServesKind(t *testing.T) {
	list := &metav1.APIResourceList{APIResources: []metav1.APIResource{{Kind: "InfisicalAuth"}, {Kind: "InfisicalConnection"}}}
	if !resourceListServesKind(list, "InfisicalAuth") {
		t.Fatal("expected InfisicalAuth to be served")
	}
	if resourceListServesKind(list, "InfisicalPushSecret") {
		t.Fatal("did not expect InfisicalPushSecret to be served")
	}
}

// stubInstall swaps the Helm install and the "kind is served" probe for the duration of a test
// and records every install the handler asks for.
func stubInstall(t *testing.T, served bool, installErr error) *[]kubernetes.ApplyHelmChartConfig {
	t.Helper()
	var calls []kubernetes.ApplyHelmChartConfig
	origApply, origServed := applyHelmChart, kindServed
	applyHelmChart = func(_ *kubernetes.Client, cfg kubernetes.ApplyHelmChartConfig) error {
		calls = append(calls, cfg)
		return installErr
	}
	kindServed = func(*kubernetes.Client, component.ComponentDefinition) bool { return served }
	t.Cleanup(func() { applyHelmChart, kindServed = origApply, origServed })
	return &calls
}

func chartComponent() component.ComponentDefinition {
	comp := compWithSource(chartURI, "")
	comp.DisplayName = "playground-secrets"
	comp.Configuration = map[string]interface{}{"metadata": map[string]interface{}{"namespace": "meshery"}}
	return comp
}

// Every registrant that can carry a Helm chart goes through the same install, and the
// registrants that cannot stay no-ops.
func TestHandleDependentsByRegistrant(t *testing.T) {
	for _, kind := range []string{"artifacthub", "github"} {
		t.Run(kind+" installs the chart", func(t *testing.T) {
			calls := stubInstall(t, false, nil)
			h, err := NewDependencyHandler(kind)
			if err != nil {
				t.Fatal(err)
			}
			summary, err := h.HandleDependents(chartComponent(), nil, true, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(*calls) != 1 {
				t.Fatalf("expected 1 install, got %d", len(*calls))
			}
			got := (*calls)[0]
			if got.URL != chartURI || got.Namespace != "meshery" || !got.CreateNamespace || got.Action != kubernetes.INSTALL || got.UpgradeIfInstalled {
				t.Fatalf("unexpected install config: %+v", got)
			}
			if summary != "Deployed dependent helm chart for playground-secrets" {
				t.Fatalf("unexpected summary %q", summary)
			}
		})
	}

	t.Run("kubernetes does nothing", func(t *testing.T) {
		calls := stubInstall(t, false, nil)
		h, _ := NewDependencyHandler("kubernetes")
		if summary, err := h.HandleDependents(chartComponent(), nil, true, false); err != nil || summary != "" {
			t.Fatalf("got (%q, %v)", summary, err)
		}
		if len(*calls) != 0 {
			t.Fatalf("expected no install, got %d", len(*calls))
		}
	})
}

func TestHandleDependentsInstallBehaviour(t *testing.T) {
	t.Run("no chart means nothing to install and no error", func(t *testing.T) {
		calls := stubInstall(t, false, nil)
		summary, err := (GitHub{}).HandleDependents(compWithSource("git://github.com/x/y/main/helm", ""), nil, true, false)
		if err != nil || summary != "" || len(*calls) != 0 {
			t.Fatalf("got (%q, %v) with %d installs", summary, err, len(*calls))
		}
	})

	t.Run("a kind the cluster already serves is not installed again", func(t *testing.T) {
		calls := stubInstall(t, true, nil)
		summary, err := (GitHub{}).HandleDependents(chartComponent(), nil, true, false)
		if err != nil || len(*calls) != 0 {
			t.Fatalf("got err %v with %d installs", err, len(*calls))
		}
		if summary == "" {
			t.Fatal("expected a summary saying the dependents are already available")
		}
	})

	t.Run("the upgrade option installs even when the kind is served", func(t *testing.T) {
		calls := stubInstall(t, true, nil)
		if _, err := (GitHub{}).HandleDependents(chartComponent(), nil, true, true); err != nil {
			t.Fatal(err)
		}
		if len(*calls) != 1 || !(*calls)[0].UpgradeIfInstalled {
			t.Fatalf("expected one upgrade-if-installed install, got %+v", *calls)
		}
	})

	t.Run("undeploy leaves the operator and its CRDs in place", func(t *testing.T) {
		calls := stubInstall(t, true, nil)
		summary, err := (GitHub{}).HandleDependents(chartComponent(), nil, false, false)
		if err != nil || summary != "" || len(*calls) != 0 {
			t.Fatalf("got (%q, %v) with %d helm calls", summary, err, len(*calls))
		}
	})

	t.Run("a failed install is reported with the error", func(t *testing.T) {
		stubInstall(t, false, errors.New("boom"))
		summary, err := (GitHub{}).HandleDependents(chartComponent(), nil, true, false)
		if err == nil || summary == "" {
			t.Fatalf("expected an error and a summary, got (%q, %v)", summary, err)
		}
	})
}
