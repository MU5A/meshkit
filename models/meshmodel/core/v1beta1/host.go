package v1beta1

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/meshery/meshkit/utils"
	"github.com/meshery/meshkit/utils/kubernetes"
	"github.com/meshery/schemas/models/v1beta1/connection"
	"github.com/meshery/schemas/models/v1beta3/component"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type MeshModelHostsWithEntitySummary struct {
	connection.Connection
	Summary EntitySummary `json:"summary"`
}
type EntitySummary struct {
	Models        int64 `json:"models"`
	Components    int64 `json:"components"`
	Relationships int64 `json:"relationships"`
	Policies      int64 `json:"policies"`
}
type MesheryHostSummaryDB struct {
	connection.Connection
	Models        int64 `json:"-" gorm:"models"`
	Components    int64 `json:"-" gorm:"components"`
	Relationships int64 `json:"-" gorm:"relationships"`
	Policies      int64 `json:"-" gorm:"policies"`
}

type HostFilter struct {
	Name        string
	Greedy      bool //when set to true - instead of an exact match, name will be prefix matched
	Trim        bool //when set to true - the schema is not returned
	DisplayName string
	Version     string
	Sort        string //asc or desc. Default behavior is asc
	OrderOn     string
	Limit       int //If 0 or  unspecified then all records are returned and limit is not used
	Offset      int
}

type DependencyHandler interface {
	HandleDependents(comp component.ComponentDefinition, kc *kubernetes.Client, isDeploy, performUpgrade bool) (string, error)
	String() string
}

func NewDependencyHandler(connectionKind string) (DependencyHandler, error) {
	switch connectionKind {
	case "kubernetes":
		return Kubernetes{}, nil
	case "artifacthub":
		return ArtifactHub{}, nil
	case "github":
		return GitHub{}, nil
	}
	return nil, ErrUnknownKind(fmt.Errorf("unknown kind %s", connectionKind))
}

// Each connection from where meshmodels can be generated needs to implement this interface
// HandleDependents, contains host specific logic for provisioning required CRDs/operators for corresponding components.

type ArtifactHub struct{}

const MesheryAnnotationPrefix = "design.meshery.io"

func (ah ArtifactHub) HandleDependents(comp component.ComponentDefinition, kc *kubernetes.Client, isDeploy, performUpgrade bool) (summary string, err error) {
	return handleHelmDependents(comp, kc, isDeploy, performUpgrade)
}

func (ah ArtifactHub) String() string {
	return "artifacthub"
}

// applyHelmChart is the Helm install used for dependents. It is a variable so that tests
// can observe the install without a cluster.
var applyHelmChart = func(kc *kubernetes.Client, cfg kubernetes.ApplyHelmChartConfig) error {
	return kc.ApplyHelmChart(cfg)
}

// kindServed reports whether the cluster already serves the component's kind, which is the
// signal that the operator and CRDs the component depends on are present. Any failure to
// find out counts as "not served", so the install is attempted rather than skipped.
var kindServed = func(kc *kubernetes.Client, comp component.ComponentDefinition) bool {
	if kc == nil || kc.KubeClient == nil || comp.Component.Version == "" || comp.Component.Kind == "" {
		return false
	}
	list, err := kc.KubeClient.Discovery().ServerResourcesForGroupVersion(comp.Component.Version)
	if err != nil || list == nil {
		return false
	}
	return resourceListServesKind(list, comp.Component.Kind)
}

func resourceListServesKind(list *metav1.APIResourceList, kind string) bool {
	for _, r := range list.APIResources {
		if r.Kind == kind {
			return true
		}
	}
	return false
}

// dependentChartURI returns the Helm chart archive a component's dependents are installed
// from, or "" when there is none. The registry keeps source_uri on the component's metadata;
// the model's metadata is consulted too for definitions that carry it there. Only http(s)
// URLs of a chart archive qualify: the same property holds repository URLs and raw manifests
// for other models, and those are not Helm charts.
func dependentChartURI(comp component.ComponentDefinition) string {
	candidates := []map[string]interface{}{comp.Metadata.AdditionalProperties}
	if comp.Model != nil && comp.Model.Metadata != nil {
		candidates = append(candidates, comp.Model.Metadata.AdditionalProperties)
	}
	for _, props := range candidates {
		uri, err := utils.Cast[string](props["source_uri"])
		if err != nil {
			continue
		}
		if isHelmChartArchive(uri) {
			return uri
		}
	}
	return ""
}

func isHelmChartArchive(uri string) bool {
	u, err := url.Parse(uri)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	path := strings.ToLower(u.Path)
	return strings.HasSuffix(path, ".tgz") || strings.HasSuffix(path, ".tar.gz")
}

// dependentNamespace is the namespace the chart is installed into: the namespace the
// component is configured for, or "" to let Helm use its default.
func dependentNamespace(comp component.ComponentDefinition) string {
	if ns, ok := comp.Configuration["namespace"].(string); ok && ns != "" {
		return ns
	}
	if meta, ok := comp.Configuration["metadata"].(map[string]interface{}); ok {
		if ns, ok := meta["namespace"].(string); ok {
			return ns
		}
	}
	return ""
}

// handleHelmDependents installs the operator and CRDs a component needs from the Helm chart
// named by its source_uri.
//
// Installing is a no-op when there is no chart to install, when the cluster already serves
// the component's kind (so several components of one model, and redeploys, do not install the
// same release again), and on undeploy: removing a design must not uninstall an operator and
// its CRDs, which would delete every custom resource of that kind in the cluster, whoever
// created it. performUpgrade overrides the "already served" check and upgrades the release.
func handleHelmDependents(comp component.ComponentDefinition, kc *kubernetes.Client, isDeploy, performUpgrade bool) (summary string, err error) {
	sourceURI := dependentChartURI(comp)
	if sourceURI == "" || !isDeploy {
		return summary, nil
	}
	if !performUpgrade && kindServed(kc, comp) {
		return fmt.Sprintf("Dependent resources for %s are already available in the cluster", comp.DisplayName), nil
	}

	err = applyHelmChart(kc, kubernetes.ApplyHelmChartConfig{
		URL:                sourceURI,
		Namespace:          dependentNamespace(comp),
		CreateNamespace:    true,
		Action:             kubernetes.INSTALL,
		UpgradeIfInstalled: performUpgrade,
	})
	if err != nil {
		return fmt.Sprintf("error deploying dependent helm chart for %s, please proceed with manual install or try again", comp.DisplayName), err
	}
	return fmt.Sprintf("Deployed dependent helm chart for %s", comp.DisplayName), nil
}

type Kubernetes struct{}

func (k Kubernetes) HandleDependents(_ component.ComponentDefinition, _ *kubernetes.Client, _, _ bool) (summary string, err error) {
	return summary, err
}

func (k Kubernetes) String() string {
	return "kubernetes"
}

type GitHub struct{}

// HandleDependents installs the Helm chart of a component whose model is
// registered from GitHub and publishes a chart archive as its source_uri.
// Models whose source_uri is anything else (a repository, a raw manifest) have
// no Helm dependent to install and are left alone.
func (gh GitHub) HandleDependents(comp component.ComponentDefinition, kc *kubernetes.Client, isDeploy, performUpgrade bool) (summary string, err error) {
	return handleHelmDependents(comp, kc, isDeploy, performUpgrade)
}

func (gh GitHub) String() string {
	return "github"
}
