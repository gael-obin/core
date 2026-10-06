# Runtime configuration and deployment declarations

Core owns the generic configuration contract shared by agents and services:

- `codefly.base.v0.Configuration` identifies an origin and runtime context.
- `ConfigurationInformation` groups string values or structured data.
- `ConfigurationValue` marks each string value as public or secret.
- A secret `ConfigurationValue` may carry a `ConfigurationValueTemplate` instead
  of a value: literals around references to the declaring producer's own
  configuration values, each reference stating an escape. Core assembles it
  wherever it delivers values itself — `ProducerConfigurationValueLookup` scopes
  resolution to that one producer, so a reference cannot name another service's
  secret, and `EvaluateConfigurationValueTemplate` is the byte-for-byte reference
  semantics a renderer must reproduce. A reference that is absent, empty or
  itself an assembly fails; it never yields an empty credential. Host support is
  declared as the `configuration-value-template/v1` operation contract.
- `ConfigurationData` carries a format, bytes and a secrecy flag. Nested JSON
  belongs here without database, cloud or Kubernetes-specific fields.
- The configuration loader resolves configured secret references without
  persisting the resulting secret values.

The CLI owns the producer-facing `codefly/coordinate/v1` deployment document,
its strict parser and its Kubernetes projection. See the
[CLI contract](https://github.com/codefly-dev/cli/blob/main/docs/configuration-contract.md).
Cluster, registry, namespace, ingress, GitOps targets, quotas, managed-service
replacements, ExternalSecret delivery and workload identity attachments are
deployment concerns. Infrastructure producers resolve their resources into that
contract; the CLI does not translate private infrastructure inventories.

Core's workspace and environment loaders preserve host-owned YAML extensions so
saving runtime configuration cannot erase deployment declarations. Core does not
interpret, validate or send these extensions to agents. The host must admit its
own declarations before acting on them.

The local `secrets` selector remains in Core because its configuration loader
actually consumes it. It is distinct from the CLI's `service-secrets` projection
into Kubernetes ExternalSecrets.

## Nested data delivery

`ConfigurationAsEnvironmentVariables(configuration, environment, secret)` returns
variables and an error. It emits flat strings unchanged and structured data through
the versioned `codefly/configuration-document/v1` JSON envelope. The scope contains
the exact origin, configuration name, environment and secrecy flag. Its environment
key hashes those identity strings without case or punctuation normalization.
Public and secret documents use separate namespaces; the decoder verifies the
complete requested scope and rejects unknown envelope fields and trailing data.

JSON objects, arrays, numbers, strings, booleans and null retain their types and
numeric precision. JSON-compatible single-document YAML is converted to JSON.
Unsupported formats, malformed input, absent scope and documents or encoded
carriers exceeding 64 KiB fail explicitly. Error messages exclude content. The
directory loader's existing `.yaml` and `.secret.yaml` names are unchanged.

`EnvironmentVariableManager.Configurations` never emits secret documents;
`Secrets` returns them separately with error propagation. Raw configuration
injection refuses structured data because it has no document identity. Promotable
GitOps rendering refuses resolved structured secrets, including configurations
containing no flat secret keys. It must carry declared external references.

A restricted render carries no secret values, so it does not assemble a
templated value: `DeliverConfigurationTemplatesByReference` omits it, and the
render is admitted only when it declares a `KubernetesSecretKeyReference` for
that value's carrier — otherwise the workload would boot with the credential
silently absent. The assembly is then delivered by the environment's secret
store, which is why the manifest contract admits an `ExternalSecret`
`spec.target.template` whose every value is an assembly over that
`ExternalSecret`'s own declared `secretKey`s, and refuses `templateFrom`,
`metadata`, a template combined with `dataFrom`, and any value that inlines
text rather than referencing a declared key.

SDK-Go exposes `ConfigurationDocument`, `SecretDocument` and workspace equivalents
plus typed decoding methods. This transport is generic runtime behavior, not a
reason to restore deployment or database models to Core. Existing flat JSON strings
remain strings and are not silently converted into documents.

A flat value larger than 32 KiB is delivered as a file whose path the
environment carries, so no value is lost to a platform's environment limit;
see [Runnable binding delivery](runnable-binding-delivery.md).

See [the boundary and migration decision](core-cli-boundary.md).

## Observability runtime handoff

`wool/otel.ReadConfiguration` reads an observability group's strings through the
consumer's framework accessor. `OBSERVABILITY_STATE` is mandatory: `enabled`
requires an HTTP(S) `OTEL_EXPORTER_OTLP_ENDPOINT`, protocol `grpc`, and a declared
collector tier; `disabled` requires `OBSERVABILITY_DISABLED_REASON`; `stdout`
is the explicit local tracing mode. Disabled and stdout modes start no network
exporter, even when a stale lower-priority endpoint remains.

`EnableConfigured` validates this state before creating a trace provider. URL
transport and exporter options remain owned by the official OpenTelemetry gRPC
exporter. The configuration reader does not choose a collector, create one,
grant network access, or modify the process environment. Infrastructure resolves
the destination; the SDK supplies the workspace accessor; the CLI delivers it.
Metrics consumers start their OTLP provider only for `enabled` state.
