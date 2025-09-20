package project

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/api/resource"
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
		a.CPUQuantity = val
	}

	// Parse and validate Memory if provided
	if a.Memory != "" {
		val, err := resource.ParseQuantity(a.Memory)
		if err != nil {
			return fmt.Errorf("error validating deploy memory value '%s'. %w", a.Memory, err)
		}
		a.MemoryQuantity = val
	}

	// Parse and validate Disk if provided
	if a.Disk != "" {
		val, err := resource.ParseQuantity(a.Disk)
		if err != nil {
			return fmt.Errorf("error validating deploy disk value '%s'. %w", a.Disk, err)
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
		a.CPUQuantity = val
	}

	// Parse and validate Memory if provided
	if a.Memory != "" {
		val, err := resource.ParseQuantity(a.Memory)
		if err != nil {
			return fmt.Errorf("error validating deploy memory value '%s'. %w", a.Memory, err)
		}
		a.MemoryQuantity = val
	}

	// Parse and validate Disk if provided
	if a.Disk != "" {
		val, err := resource.ParseQuantity(a.Disk)
		if err != nil {
			return fmt.Errorf("error validating deploy disk value '%s'. %w", a.Disk, err)
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

// shared logic
func (a *Trigger) decodeMap(raw map[string]any) error {
	if t, ok := raw["type"].(string); ok {
		switch t {
		case "api":
			a.Type = TriggerTypeAPI
		case "webhook":
			a.Type = TriggerTypeWebhook
		case "cron":
			a.Type = TriggerTypeCron
		case "manual":
			a.Type = TriggerTypeManual
		case "agent":
			a.Type = TriggerTypeAgent
		case "sms":
			a.Type = TriggerTypeSMS
		case "email":
			a.Type = TriggerTypeEmail
		default:
			return fmt.Errorf("unknown trigger type: %s. should be one of api, webhook, cron, manual, agent, sms, or email", t)
		}
	}
	if d, ok := raw["destination"].(string); ok {
		switch d {
		case "source":
			a.Destination = TriggerDirectionSource
		case "destination":
			a.Destination = TriggerDirectionDestination
		default:
			return fmt.Errorf("unknown trigger destination: %s. should be one of source or destination", d)
		}
	}
	delete(raw, "type")
	delete(raw, "destination")
	a.Fields = raw
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

// shared logic
func (a *Authentication) decodeMap(raw map[string]any) error {
	if t, ok := raw["type"].(string); ok {
		switch t {
		case "project":
			a.Type = AuthenticationTypeProject
		case "bearer":
			a.Type = AuthenticationTypeBearer
		case "basic":
			a.Type = AuthenticationTypeBasic
		case "header":
			a.Type = AuthenticationTypeHeader
		default:
			return fmt.Errorf("unknown authentication type: %s. should be one of project, bearer, basic, or header", t)
		}
	}
	delete(raw, "type")
	a.Fields = raw
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
