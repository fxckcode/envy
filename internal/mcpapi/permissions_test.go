package mcpapi_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fxckcode/envy/internal/config"
	"github.com/fxckcode/envy/internal/project"
	"github.com/fxckcode/envy/internal/redact"
)

func setAgentPermissions(p *project.Project, actor, env string, metadata, readValues bool) {
	if p.Config().Agents[actor] == nil {
		p.Config().Agents[actor] = map[string]config.AgentPerms{}
	}
	p.Config().Agents[actor][env] = config.AgentPerms{Metadata: metadata, ReadValues: readValues}
}

func TestReadValuesGatesAllMCPValuePayloads(t *testing.T) {
	svc, p := openSvc(t, "default")
	for _, env := range []string{"development", "staging", "production"} {
		setAgentPermissions(p, "default", env, true, false)
	}

	list, err := svc.List("development")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	region, regionExists := p.RawValue("development", "AWS_REGION")
	_, secretExists := p.RawValue("development", "DATABASE_URL")
	foundRegion, foundSecret := false, false
	for _, key := range list.Keys {
		if key.Key == "AWS_REGION" {
			foundRegion = true
			if key.Value != "" {
				t.Fatal("List exposed non-secret value without read_values")
			}
		}
		if key.Key == "DATABASE_URL" {
			foundSecret = true
			if key.Value != redact.MCPPlaceholder {
				t.Fatalf("List secret value was not redacted: %q", key.Value)
			}
		}
	}
	if !regionExists || !secretExists || !foundRegion || !foundSecret || region == "" {
		t.Fatal("List fixture is missing expected configured keys")
	}

	metadata, err := svc.Metadata("development", "AWS_REGION")
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	if metadata.Value != "" {
		t.Fatalf("Metadata exposed non-secret value without read_values: %q", metadata.Value)
	}
	secret, err := svc.Metadata("development", "DATABASE_URL")
	if err != nil {
		t.Fatalf("secret metadata: %v", err)
	}
	if secret.Value != redact.MCPPlaceholder {
		t.Fatalf("Metadata secret value was not redacted: %q", secret.Value)
	}

	diff, err := svc.Diff("staging", "production")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	for _, env := range []string{"staging", "production"} {
		want, ok := p.RawValue(env, "AWS_REGION")
		if !ok || want == "" {
			t.Fatalf("fixture is missing AWS_REGION in %s", env)
		}
		cell := diff.Cells["AWS_REGION"][env]
		if cell.Value != "" || cell.Display == want {
			t.Fatalf("Diff exposed %s value without read_values", env)
		}
	}
	if got := diff.Cells["STRIPE_SECRET"]["staging"].Value; got != redact.MCPPlaceholder {
		t.Fatalf("Diff secret value was not redacted: %q", got)
	}

	if value, err := svc.ReadValue("development", "AWS_REGION"); err == nil || value != "" {
		t.Fatalf("ReadValue without read_values = %q, %v; want denial", value, err)
	}
}

func TestReadValuesPermissionAllowsValuesIncludingSecrets(t *testing.T) {
	svc, p := openSvc(t, "default")
	for _, env := range []string{"development", "staging", "production"} {
		setAgentPermissions(p, "default", env, true, true)
	}

	list, err := svc.List("development")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	region, regionExists := p.RawValue("development", "AWS_REGION")
	secret, secretExists := p.RawValue("development", "DATABASE_URL")
	foundRegion, foundSecret := false, false
	for _, key := range list.Keys {
		switch key.Key {
		case "AWS_REGION":
			foundRegion = key.Value == region
		case "DATABASE_URL":
			foundSecret = key.Value == secret
		}
	}
	if !regionExists || !secretExists || region == "" || secret == "" || !foundRegion || !foundSecret {
		t.Fatalf("ReadValues-enabled list did not return permitted values: region=%v secret=%v", foundRegion, foundSecret)
	}

	metadata, err := svc.Metadata("development", "DATABASE_URL")
	if err != nil || metadata.Value != secret {
		t.Fatalf("ReadValues-enabled metadata omitted the permitted value: err=%v", err)
	}
	value, err := svc.ReadValue("development", "DATABASE_URL")
	if err != nil || value != secret {
		t.Fatalf("ReadValues-enabled read omitted the permitted value: err=%v", err)
	}

	diff, err := svc.Diff("staging", "production")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	productionRegion, ok := p.RawValue("production", "AWS_REGION")
	if got := diff.Cells["AWS_REGION"]["production"].Value; !ok || got != productionRegion {
		t.Fatal("ReadValues-enabled diff omitted the permitted value")
	}
	stagingSecret, ok := p.RawValue("staging", "STRIPE_SECRET")
	if got := diff.Cells["STRIPE_SECRET"]["staging"].Value; !ok || got != stagingSecret {
		t.Fatal("ReadValues-enabled diff omitted the permitted secret value")
	}
}

func TestDiffAppliesReadValuesPerEnvironment(t *testing.T) {
	svc, p := openSvc(t, "default")
	setAgentPermissions(p, "default", "staging", true, true)
	setAgentPermissions(p, "default", "production", true, false)

	diff, err := svc.Diff("staging", "production")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	stagingRegion, stagingOK := p.RawValue("staging", "AWS_REGION")
	productionRegion, productionOK := p.RawValue("production", "AWS_REGION")
	if !stagingOK || diff.Cells["AWS_REGION"]["staging"].Value != stagingRegion {
		t.Fatal("Diff omitted a value permitted in staging")
	}
	productionCell := diff.Cells["AWS_REGION"]["production"]
	if !productionOK || productionCell.Value != "" || productionCell.Display == productionRegion {
		t.Fatal("Diff exposed a value denied in production")
	}
}

func TestMetadataPermissionGatesEnvironmentDiscoveryAndReads(t *testing.T) {
	svc, p := openSvc(t, "default")
	setAgentPermissions(p, "default", "development", false, true)

	if _, err := svc.List("development"); err == nil {
		t.Fatal("List should deny an environment without metadata permission")
	}
	if _, err := svc.Metadata("development", "DATABASE_URL"); err == nil {
		t.Fatal("Metadata should deny an environment without metadata permission")
	}
	if _, err := svc.Check("development"); err == nil {
		t.Fatal("Check should deny an environment without metadata permission")
	}
	if _, err := svc.Exists("development", "missing-key"); err == nil || !strings.Contains(err.Error(), "permission") {
		t.Fatalf("Exists should deny before exposing key presence, got %v", err)
	}
	if _, err := svc.ReadValue("development", "missing-key"); err == nil || !strings.Contains(err.Error(), "permission") {
		t.Fatalf("ReadValue should deny before exposing key presence, got %v", err)
	}

	environments := svc.ListEnvironments().Environments
	for _, env := range environments {
		if env.Name == "development" {
			t.Fatal("ListEnvironments exposed a metadata-denied environment")
		}
	}
	if schema := svc.GetSchema(); len(schema.Fields) != 0 {
		t.Fatalf("GetSchema should fail closed when any environment denies metadata: %d fields", len(schema.Fields))
	}

	for _, pair := range [][2]string{{"development", "staging"}, {"staging", "development"}} {
		if _, err := svc.Diff(pair[0], pair[1]); err == nil {
			t.Fatalf("Diff should deny metadata access for either side: %v", pair)
		}
	}
}

func TestReadValuesPermissionDoesNotReplaceMetadataPermission(t *testing.T) {
	svc, p := openSvc(t, "default")
	setAgentPermissions(p, "default", "development", false, true)
	if _, err := svc.ReadValue("development", "DATABASE_URL"); err == nil {
		t.Fatal("ReadValues must not bypass the Metadata permission")
	}
}

func TestListEnvironmentsReturnsOnlyMetadataAllowedEnvironments(t *testing.T) {
	svc, p := openSvc(t, "default")
	setAgentPermissions(p, "default", "development", false, false)
	setAgentPermissions(p, "default", "staging", true, false)
	res := svc.ListEnvironments()
	if len(res.Environments) != 2 {
		t.Fatalf("ListEnvironments returned %d environments, want 2", len(res.Environments))
	}
}

func TestGetSchemaRequiresMetadataAndRedactsValues(t *testing.T) {
	svc, p := openSvc(t, "default")
	for _, env := range []string{"development", "staging", "production"} {
		setAgentPermissions(p, "default", env, true, false)
	}

	schema := svc.GetSchema()
	var port, nodeEnv *project.SchemaFieldView
	for i := range schema.Fields {
		field := &schema.Fields[i]
		switch field.Key {
		case "PORT":
			port = field
		case "NODE_ENV":
			nodeEnv = field
		}
	}
	if port == nil || port.Type != "integer" || !port.Required {
		t.Fatalf("GetSchema omitted permitted field metadata: PORT=%+v", port)
	}
	if port.Default != "" || len(port.Values) != 0 || port.Constraints == nil || port.Constraints.Min == nil || port.Constraints.Max == nil || len(port.Constraints.Values) != 0 {
		t.Fatalf("GetSchema exposed value metadata without read_values: %+v", port)
	}
	if nodeEnv == nil || nodeEnv.Type != "enum" || nodeEnv.Required || len(nodeEnv.Values) != 0 || nodeEnv.Constraints == nil || len(nodeEnv.Constraints.Values) != 0 {
		t.Fatalf("GetSchema did not retain metadata while omitting enum values: %+v", nodeEnv)
	}

	for _, env := range []string{"development", "staging", "production"} {
		setAgentPermissions(p, "default", env, true, true)
	}
	permitted := svc.GetSchema()
	for _, field := range permitted.Fields {
		if field.Key == "PORT" && field.Default != "3000" {
			t.Fatalf("ReadValues-enabled schema omitted default: %+v", field)
		}
		if field.Key == "NODE_ENV" && (len(field.Values) == 0 || field.Constraints == nil || len(field.Constraints.Values) == 0) {
			t.Fatalf("ReadValues-enabled schema omitted enum values: %+v", field)
		}
	}

	setAgentPermissions(p, "default", "development", false, true)
	denied := svc.GetSchema()
	if len(denied.Fields) != 0 {
		t.Fatalf("GetSchema exposed fields when metadata was denied: %d", len(denied.Fields))
	}
	assertMetadataDenialSignal(t, denied)
}

func TestGenerateExampleRequiresMetadataAndReadValues(t *testing.T) {
	svc, p := openSvc(t, "default")
	for _, env := range []string{"development", "staging", "production"} {
		setAgentPermissions(p, "default", env, true, false)
	}

	example := svc.GenerateExample()
	if _, ok := example.Payload["PORT"]; !ok {
		t.Fatal("GenerateExample omitted a permitted metadata key")
	}
	if example.Payload["PORT"] != "" {
		t.Fatalf("GenerateExample exposed a default without read_values: %q", example.Payload["PORT"])
	}

	for _, env := range []string{"development", "staging", "production"} {
		setAgentPermissions(p, "default", env, true, true)
	}
	if got := svc.GenerateExample().Payload["PORT"]; got != "3000" {
		t.Fatalf("ReadValues-enabled example omitted default: %q", got)
	}

	setAgentPermissions(p, "default", "development", false, true)
	denied := svc.GenerateExample()
	if len(denied.Payload) != 0 {
		t.Fatalf("GenerateExample exposed payload when metadata was denied: %+v", denied.Payload)
	}
	assertMetadataDenialSignal(t, denied)
}

func TestDoctorRequiresMetadata(t *testing.T) {
	svc, p := openSvc(t, "default")
	setAgentPermissions(p, "default", "development", false, true)

	denied := svc.Doctor()
	if len(denied.Checks) != 0 || denied.Score != 0 {
		t.Fatalf("Doctor exposed status when metadata was denied: %+v", denied)
	}
	assertMetadataDenialSignal(t, denied)
}

func TestCopyDeniesWhenSourceReadValuesIsNotGranted(t *testing.T) {
	svc, p := openSvc(t, "writer-agent")
	setAgentPermissions(p, "writer-agent", "staging", true, false)

	before, exists := p.RawValue("development", "DEBUG")
	if !exists {
		t.Fatal("development fixture is missing DEBUG")
	}
	source, exists := p.RawValue("staging", "DEBUG")
	if !exists || source == before {
		t.Fatalf("fixture must provide a distinct staging DEBUG value: source=%q target=%q present=%v", source, before, exists)
	}
	pendingBefore := len(p.PendingApprovals())

	result, err := svc.Copy("staging", "development", "DEBUG")
	if err == nil || result.OK || result.Status != "denied" {
		t.Fatalf("copy without source read_values = %+v, %v; want denied", result, err)
	}
	if after, ok := p.RawValue("development", "DEBUG"); !ok || after != before {
		t.Fatalf("denied copy changed target: before=%q after=%q present=%v", before, after, ok)
	}
	if got := len(p.PendingApprovals()); got != pendingBefore {
		t.Fatalf("denied copy queued approval: before=%d after=%d", pendingBefore, got)
	}

	audit := p.AuditLog()
	if len(audit) == 0 {
		t.Fatal("denied copy was not audited")
	}
	last := audit[len(audit)-1]
	if last.Action != "env_copy" || last.Environment != "development" || last.Result != project.AuditDenied {
		t.Fatalf("last audit entry = %+v; want denied env_copy for development", last)
	}
}

func TestCopyAllowsSourceReadValuesWithoutSourceMetadata(t *testing.T) {
	svc, p := openSvc(t, "writer-agent")
	p.Config().Agents["writer-agent"]["staging"] = config.AgentPerms{ReadValues: true}

	source, exists := p.RawValue("staging", "DEBUG")
	if !exists {
		t.Fatal("staging fixture is missing DEBUG")
	}
	result, err := svc.Copy("staging", "development", "DEBUG")
	if err != nil || !result.OK {
		t.Fatalf("permitted copy = %+v, %v; want success", result, err)
	}
	if got, ok := p.RawValue("development", "DEBUG"); !ok || got != source {
		t.Fatalf("copied target value = %q present=%v; want source value %q", got, ok, source)
	}
}

func assertMetadataDenialSignal(t *testing.T, response any) {
	t.Helper()
	blob, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal denied response: %v", err)
	}
	if !strings.Contains(string(blob), `"error"`) || !strings.Contains(string(blob), "metadata") {
		t.Fatalf("denied response lacks an explicit metadata error: %s", blob)
	}
}
