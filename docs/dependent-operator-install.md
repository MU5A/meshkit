# Installing the operator a component depends on

When a design is deployed, Meshery Server asks the dependency handler for each component to
provision what the component needs in the cluster first: the operator and CRDs behind a custom
resource. The server skips this when the deploy is made with `skipCRD`.

The handler is picked by the registrant of the component's model
(`models/meshmodel/core/v1beta1/host.go`):

| Registrant  | Behaviour |
|-------------|-----------|
| `artifacthub`, `github` | Install the component's Helm chart, as described below. |
| `kubernetes` | Nothing to install. |

## Which chart

The chart is the component's `source_uri`, read from the component's metadata (where the
registry keeps it) and, failing that, from the model's metadata. Only an http(s) URL of a chart
archive (`.tgz` or `.tar.gz`) counts. The same property holds a repository URL or a raw manifest
for other GitHub-registered models; those have no Helm dependent and are left alone.

## When it installs

- The chart is installed into the namespace the component is configured for (top-level
  `namespace`, or `metadata.namespace`), creating it if needed. With neither, Helm's default
  applies. Everything the chart creates goes to that namespace along with its release record
  (see `utils/kubernetes/namespaced-rest-client-getter.go`).
- If the cluster already serves the component's kind, nothing is installed. That keeps a design
  with several components of one model, and a redeploy, from installing the same release again.
- The `upgrade` option installs regardless and upgrades the release if it exists.
- Undeploying a design does not uninstall the chart. Removing an operator removes its CRDs and
  with them every custom resource of that kind in the cluster, whoever created it.

An install failure is reported on the component's prerequisite entry and does not stop the
component itself from being applied.
