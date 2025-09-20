package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/agentuity/go-common/sys"
	yc "github.com/zijiren233/yaml-comment"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/api/resource"
)

var (
	ErrProjectNotFound         = errors.New("project not found")
	ErrProjectMissingProjectId = errors.New("missing project_id value")
)

type Resources struct {
	Memory string `json:"memory,omitempty" yaml:"memory,omitempty" hc:"The memory requirements"`
	CPU    string `json:"cpu,omitempty" yaml:"cpu,omitempty" hc:"The CPU requirements"`
	Disk   string `json:"disk,omitempty" yaml:"disk,omitempty" hc:"The disk size requirements"`

	CPUQuantity    resource.Quantity `json:"-" yaml:"-"`
	MemoryQuantity resource.Quantity `json:"-" yaml:"-"`
	DiskQuantity   resource.Quantity `json:"-" yaml:"-"`
}

func (a *Resources) UnmarshalJSON(data []byte) error {
	type Alias Resources
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(a),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	// Parse and validate CPU if provided
	if a.CPU != "" {
		val, err := resource.ParseQuantity(a.CPU)
		if err != nil {
			return fmt.Errorf("error validating deploy cpu value '%s'. %w", a.CPU, err)
		}
		if val.Sign() < 0 {
			return fmt.Errorf("resource CPU must be >= 0, got '%s'", a.CPU)
		}
		a.CPUQuantity = val
	}

	// Parse and validate Memory if provided
	if a.Memory != "" {
		val, err := resource.ParseQuantity(a.Memory)
		if err != nil {
			return fmt.Errorf("error validating deploy memory value '%s'. %w", a.Memory, err)
		}
		if val.Sign() < 0 {
			return fmt.Errorf("resource Memory must be >= 0, got '%s'", a.Memory)
		}
		a.MemoryQuantity = val
	}

	// Parse and validate Disk if provided
	if a.Disk != "" {
		val, err := resource.ParseQuantity(a.Disk)
		if err != nil {
			return fmt.Errorf("error validating deploy disk value '%s'. %w", a.Disk, err)
		}
		if val.Sign() < 0 {
			return fmt.Errorf("resource Disk must be >= 0, got '%s'", a.Disk)
		}
		a.DiskQuantity = val
	}

	return nil
}

func (a *Resources) UnmarshalYAML(value *yaml.Node) error {
	// First unmarshal into the struct normally
	type ResourcesAlias struct {
		Memory string `yaml:"memory,omitempty"`
		CPU    string `yaml:"cpu,omitempty"`
		Disk   string `yaml:"disk,omitempty"`
	}

	var aux ResourcesAlias
	if err := value.Decode(&aux); err != nil {
		return err
	}

	// Copy the values
	a.Memory = aux.Memory
	a.CPU = aux.CPU
	a.Disk = aux.Disk

	// Parse and validate CPU if provided
	if a.CPU != "" {
		val, err := resource.ParseQuantity(a.CPU)
		if err != nil {
			return fmt.Errorf("error validating deploy cpu value '%s'. %w", a.CPU, err)
		}
		if val.Sign() < 0 {
			return fmt.Errorf("resource CPU must be >= 0, got '%s'", a.CPU)
		}
		a.CPUQuantity = val
	}

	// Parse and validate Memory if provided
	if a.Memory != "" {
		val, err := resource.ParseQuantity(a.Memory)
		if err != nil {
			return fmt.Errorf("error validating deploy memory value '%s'. %w", a.Memory, err)
		}
		if val.Sign() < 0 {
			return fmt.Errorf("resource Memory must be >= 0, got '%s'", a.Memory)
		}
		a.MemoryQuantity = val
	}

	// Parse and validate Disk if provided
	if a.Disk != "" {
		val, err := resource.ParseQuantity(a.Disk)
		if err != nil {
			return fmt.Errorf("error validating deploy disk value '%s'. %w", a.Disk, err)
		}
		if val.Sign() < 0 {
			return fmt.Errorf("resource Disk must be >= 0, got '%s'", a.Disk)
		}
		a.DiskQuantity = val
	}

	return nil
}

type Mode struct {
	Type string  `json:"type" yaml:"type" hc:"on-demand or provisioned"`                                       // on-demand or provisioned
	Idle *string `json:"idle,omitempty" yaml:"idle,omitempty" hc:"duration in seconds if on-demand, optional"` // duration in seconds if on-demand, optional
}

type Deployment struct {
	Command      string     `json:"command" yaml:"command"`
	Args         []string   `json:"args" yaml:"args"`
	Resources    *Resources `json:"resources" yaml:"resources" hc:"You should tune the resources for the deployment"`
	Mode         *Mode      `json:"mode,omitempty" yaml:"mode,omitempty" hc:"The deployment mode"`
	Dependencies []string   `json:"dependencies,omitempty" yaml:"dependencies,omitempty" hc:"The dependencies to install before running the deployment"`
	DomainNames  []string   `yaml:"domains,omitempty" json:"domains,omitempty"`
}

type Watch struct {
	Enabled bool     `json:"enabled" yaml:"enabled" hc:"Whether to watch for changes and automatically restart the server"`
	Files   []string `json:"files" yaml:"files" hc:"Rules for files to watch for changes"`
}

type Development struct {
	Port    int      `json:"port" yaml:"port" hc:"The port to run the development server on which can be overridden by setting the PORT environment variable"`
	Watch   Watch    `json:"watch" yaml:"watch"`
	Command string   `json:"command" yaml:"command" hc:"The command to run the development server"`
	Args    []string `json:"args" yaml:"args" hc:"The arguments to pass to the development server"`
}

type TriggerType string

const (
	TriggerTypeAPI     TriggerType = "api"
	TriggerTypeWebhook TriggerType = "webhook"
	TriggerTypeCron    TriggerType = "cron"
	TriggerTypeManual  TriggerType = "manual"
	TriggerTypeAgent   TriggerType = "agent"
	TriggerTypeSMS     TriggerType = "sms"
	TriggerTypeEmail   TriggerType = "email"
)

type TriggerDirection string

const (
	TriggerDirectionSource      TriggerDirection = "source"
	TriggerDirectionDestination TriggerDirection = "destination"
)

type Trigger struct {
	Type        TriggerType      `json:"-" yaml:"-"`
	Destination TriggerDirection `json:"-" yaml:"-"`
	Fields      map[string]any   `json:"-" yaml:"-"`
}

func (a *Trigger) UnmarshalYAML(value *yaml.Node) error {
	var raw map[string]any
	if err := value.Decode(&raw); err != nil {
		return err
	}
	return a.decodeMap(raw)
}

// JSON
func (a *Trigger) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	return a.decodeMap(raw)
}

// MarshalJSON implements json.Marshaler
func (a Trigger) MarshalJSON() ([]byte, error) {
	data := a.encodeMap()
	return json.Marshal(data)
}

// MarshalYAML implements yaml.Marshaler
func (a Trigger) MarshalYAML() (interface{}, error) {
	return a.encodeMap(), nil
}

// shared logic for encoding
func (a Trigger) encodeMap() map[string]any {
	result := make(map[string]any)

	// Copy Fields first
	if a.Fields != nil {
		for k, v := range a.Fields {
			result[k] = v
		}
	}

	// Add type if set
	if a.Type != TriggerType("") {
		result["type"] = string(a.Type)
	}

	// Add destination if set
	if a.Destination != TriggerDirection("") {
		result["destination"] = string(a.Destination)
	}

	return result
}

// shared logic
func (a *Trigger) decodeMap(raw map[string]any) error {
	// Require "type" field to exist and be a string
	typeVal, typeExists := raw["type"]
	if !typeExists {
		return fmt.Errorf("trigger type is required")
	}

	t, ok := typeVal.(string)
	if !ok {
		return fmt.Errorf("trigger type must be a string, got %T", typeVal)
	}

	var triggerType TriggerType
	switch t {
	case "api":
		triggerType = TriggerTypeAPI
	case "webhook":
		triggerType = TriggerTypeWebhook
	case "cron":
		triggerType = TriggerTypeCron
	case "manual":
		triggerType = TriggerTypeManual
	case "agent":
		triggerType = TriggerTypeAgent
	case "sms":
		triggerType = TriggerTypeSMS
	case "email":
		triggerType = TriggerTypeEmail
	default:
		return fmt.Errorf("unknown trigger type: %s. should be one of api, webhook, cron, manual, agent, sms, or email", t)
	}

	// Validate "destination" field if present
	var triggerDestination TriggerDirection
	if destVal, destExists := raw["destination"]; destExists {
		d, ok := destVal.(string)
		if !ok {
			return fmt.Errorf("trigger destination must be a string, got %T", destVal)
		}

		switch d {
		case "source":
			triggerDestination = TriggerDirectionSource
		case "destination":
			triggerDestination = TriggerDirectionDestination
		default:
			return fmt.Errorf("unknown trigger destination: %s. should be one of source or destination", d)
		}
	}

	// Only assign and delete keys after successful validation
	a.Type = triggerType
	a.Destination = triggerDestination

	// Create a copy of raw without type and destination for Fields
	fields := make(map[string]any)
	for k, v := range raw {
		if k != "type" && k != "destination" {
			fields[k] = v
		}
	}
	a.Fields = fields

	return nil
}

type AgentConfig struct {
	ID             string         `json:"id" yaml:"id" hc:"The ID of the Agent which is automatically generated"`
	Name           string         `json:"name" yaml:"name" hc:"The name of the Agent which is editable"`
	Description    string         `json:"description,omitempty" yaml:"description,omitempty" hc:"The description of the Agent which is editable"`
	Authentication Authentication `json:"authentication" yaml:"authentication" hc:"The authentication configuration for the Agent"`
	Triggers       []Trigger      `json:"triggers" yaml:"triggers" hc:"The triggers for the Agent"`
}

type Bundler struct {
	Enabled     bool               `yaml:"enabled" json:"enabled"`
	Identifier  string             `yaml:"identifier" json:"identifier"`
	Language    string             `yaml:"language" json:"language"`
	Framework   string             `yaml:"framework,omitempty" json:"framework,omitempty"`
	Runtime     string             `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	AgentConfig AgentBundlerConfig `yaml:"agents" json:"agents"`
	Ignore      []string           `yaml:"ignore,omitempty" json:"ignore,omitempty"`
	CLIVersion  string             `yaml:"-" json:"-"`
}

type AgentBundlerConfig struct {
	Dir string `yaml:"dir" json:"dir"`
}

type DeploymentConfig struct {
	Provider   string   `yaml:"provider" json:"provider"`
	Language   string   `yaml:"language" json:"language"`
	Runtime    string   `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	MinVersion string   `yaml:"min_version,omitempty" json:"min_version,omitempty"`
	WorkingDir string   `yaml:"working_dir,omitempty" json:"working_dir,omitempty"`
	Command    []string `yaml:"command,omitempty" json:"command,omitempty"`
	Env        []string `yaml:"env,omitempty" json:"env,omitempty"`
}

type AuthenticationType string

const (
	AuthenticationTypeProject AuthenticationType = "project"
	AuthenticationTypeBearer  AuthenticationType = "bearer"
	AuthenticationTypeBasic   AuthenticationType = "basic"
	AuthenticationTypeHeader  AuthenticationType = "header"
)

// Authentication has a fixed Type plus arbitrary other fields
type Authentication struct {
	Type   AuthenticationType `json:"-" yaml:"-"`
	Fields map[string]any     `json:"-" yaml:"-"`
}

func (a *Authentication) UnmarshalYAML(value *yaml.Node) error {
	var raw map[string]any
	if err := value.Decode(&raw); err != nil {
		return err
	}
	return a.decodeMap(raw)
}

// JSON
func (a *Authentication) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	return a.decodeMap(raw)
}

// MarshalJSON implements json.Marshaler
func (a Authentication) MarshalJSON() ([]byte, error) {
	data := a.encodeMap()
	return json.Marshal(data)
}

// MarshalYAML implements yaml.Marshaler
func (a Authentication) MarshalYAML() (interface{}, error) {
	return a.encodeMap(), nil
}

// shared logic for encoding
func (a Authentication) encodeMap() map[string]any {
	result := make(map[string]any)

	// Copy Fields first (handle nil case)
	if a.Fields != nil {
		for k, v := range a.Fields {
			result[k] = v
		}
	}

	// Add type if set
	if a.Type != AuthenticationType("") {
		result["type"] = string(a.Type)
	}

	return result
}

// shared logic
func (a *Authentication) decodeMap(raw map[string]any) error {
	// Require "type" field to exist and be a string
	typeVal, typeExists := raw["type"]
	if !typeExists {
		return fmt.Errorf("authentication type is required")
	}

	t, ok := typeVal.(string)
	if !ok {
		return fmt.Errorf("authentication type must be a string, got %T", typeVal)
	}

	var authenticationType AuthenticationType
	switch t {
	case "project":
		authenticationType = AuthenticationTypeProject
	case "bearer":
		authenticationType = AuthenticationTypeBearer
	case "basic":
		authenticationType = AuthenticationTypeBasic
	case "header":
		authenticationType = AuthenticationTypeHeader
	default:
		return fmt.Errorf("unknown authentication type: %s. should be one of project, bearer, basic, or header", t)
	}

	// Only assign and delete keys after successful validation
	a.Type = authenticationType

	// Create a copy of raw without type for Fields
	fields := make(map[string]any)
	for k, v := range raw {
		if k != "type" {
			fields[k] = v
		}
	}
	a.Fields = fields

	return nil
}

type Project struct {
	Version     string        `json:"version" yaml:"version" hc:"The version semver range required to run this project"`
	ProjectId   string        `json:"project_id" yaml:"project_id" hc:"The ID of the project which is automatically generated"`
	Name        string        `json:"name" yaml:"name" hc:"The name of the project which is editable"`
	Description string        `json:"description" yaml:"description" hc:"The description of the project which is editable"`
	Development *Development  `json:"development,omitempty" yaml:"development,omitempty" hc:"The development configuration for the project"`
	Deployment  *Deployment   `json:"deployment,omitempty" yaml:"deployment,omitempty"`
	Bundler     *Bundler      `json:"bundler,omitempty" yaml:"bundler,omitempty" hc:"You should not need to change these value"`
	Agents      []AgentConfig `json:"agents" yaml:"agents" hc:"The agents that are part of this project"`
}

// SafeFilename returns a safe filename for the project.
func (p *Project) SafeFilename() string {
	return SafeProjectFilename(p.Name, p.IsPython())
}

// IsPython returns true if the project is a Python project.
func (p *Project) IsPython() bool {
	return p.Bundler.Language == "python" || p.Bundler.Language == "py"
}

// IsJavaScript returns true if the project is a JavaScript project.
func (p *Project) IsJavaScript() bool {
	switch p.Bundler.Language {
	case "javascript", "js":
		return true
	case "typescript", "ts":
		return true
	default:
		return false
	}
}

// Load will load the project from a file in the given directory.
func (p *Project) Load(dir string) error {
	fn := GetProjectFilename(dir)
	if !sys.Exists(fn) {
		return ErrProjectNotFound
	}
	of, err := os.Open(fn)
	if err != nil {
		return fmt.Errorf("failed to open project file: %s. %w", fn, err)
	}
	defer of.Close()
	if err := yaml.NewDecoder(of).Decode(p); err != nil {
		return fmt.Errorf("failed to decode YAML project file: %s. %w", fn, err)
	}
	if p.ProjectId == "" {
		return ErrProjectMissingProjectId
	}
	if p.Bundler == nil {
		return fmt.Errorf("missing bundler value, please run `agentuity new` to create a new project")
	}
	if p.Bundler.Language == "" {
		return fmt.Errorf("missing bundler.language value, please run `agentuity new` to create a new project")
	}
	switch p.Bundler.Language {
	case "js", "javascript", "typescript":
		if p.Bundler.Runtime != "bunjs" && p.Bundler.Runtime != "nodejs" {
			return fmt.Errorf("invalid bundler.runtime value: %s. only bunjs and nodejs are supported", p.Bundler.Runtime)
		}
	case "py", "python":
		if p.Bundler.Runtime != "uv" {
			return fmt.Errorf("invalid bundler.runtime value: %s. only uv is supported", p.Bundler.Runtime)
		}
	default:
		return fmt.Errorf("invalid bundler.language value: %s. only js or py are supported", p.Bundler.Language)
	}
	if p.Bundler.AgentConfig.Dir == "" {
		return fmt.Errorf("missing bundler.Agents.dir value (or its empty), please run `agentuity new` to create a new project")
	}
	if p.Deployment != nil {
		if p.Deployment.Resources != nil {
			val, err := resource.ParseQuantity(p.Deployment.Resources.CPU)
			if err != nil {
				return fmt.Errorf("error validating deploy cpu value '%s'. %w", p.Deployment.Resources.CPU, err)
			}
			p.Deployment.Resources.CPUQuantity = val
			val, err = resource.ParseQuantity(p.Deployment.Resources.Memory)
			if err != nil {
				return fmt.Errorf("error validating deploy memory value '%s'. %w", p.Deployment.Resources.Memory, err)
			}
			p.Deployment.Resources.MemoryQuantity = val
			val, err = resource.ParseQuantity(p.Deployment.Resources.Disk)
			if err != nil {
				return fmt.Errorf("error validating deploy disk value '%s'. %w", p.Deployment.Resources.Disk, err)
			}
			p.Deployment.Resources.DiskQuantity = val
		}
		if p.Deployment.Mode != nil {
			if p.Deployment.Mode.Type != "on-demand" && p.Deployment.Mode.Type != "provisioned" {
				return fmt.Errorf("invalid deployment mode value: %s. only on-demand or provisioned are supported", p.Deployment.Mode.Type)
			}
		}
	}
	return nil
}

// Save will save the project to a file in the given directory.
func (p *Project) Save(dir string) error {
	fn := GetProjectFilename(dir)
	of, err := os.Create(fn)
	if err != nil {
		return err
	}
	defer of.Close()
	of.WriteString("# yaml-language-server: $schema=https://raw.githubusercontent.com/agentuity/cli/refs/heads/main/agentuity.schema.json\n")
	of.WriteString("\n")
	of.WriteString("# ------------------------------------------------\n")
	of.WriteString("# This file is generated by Agentuity\n")
	of.WriteString("# You should check this file into version control\n")
	of.WriteString("# ------------------------------------------------\n")
	of.WriteString("\n")
	enc := yaml.NewEncoder(of)
	enc.SetIndent(2)
	yenc := yc.NewEncoder(enc)
	return yenc.Encode(p)
}
