package main

// OpenAPI Doc structure definitions for YAML serialization

type OpenAPIDoc struct {
	OpenAPI    string                `yaml:"openapi"`
	Info       Info                  `yaml:"info"`
	Servers    []Server              `yaml:"servers"`
	Security   []map[string][]string `yaml:"security"`
	Tags       []Tag                 `yaml:"tags"`
	Paths      map[string]PathItem   `yaml:"paths"`
	Components Components            `yaml:"components"`
}

type Info struct {
	Title       string  `yaml:"title"`
	Version     string  `yaml:"version"`
	Description string  `yaml:"description"`
	Contact     Contact `yaml:"contact"`
}

type Contact struct {
	Name string `yaml:"name"`
}

type Server struct {
	URL         string `yaml:"url"`
	Description string `yaml:"description"`
}

type Tag struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

type PathItem map[string]*Operation

type Operation struct {
	Tags        []string              `yaml:"tags"`
	OperationID string                `yaml:"operationId"`
	Summary     string                `yaml:"summary"`
	Description string                `yaml:"description,omitempty"`
	Security    []map[string][]string `yaml:"security,omitempty"`
	Parameters  []Parameter           `yaml:"parameters,omitempty"`
	RequestBody *RequestBody          `yaml:"requestBody,omitempty"`
	Responses   map[string]Response   `yaml:"responses"`
}

type Parameter struct {
	Name        string    `yaml:"name"`
	In          string    `yaml:"in"` // "path", "query", "header"
	Required    bool      `yaml:"required,omitempty"`
	Description string    `yaml:"description,omitempty"`
	Schema      *Property `yaml:"schema,omitempty"`
}

type RequestBody struct {
	Required    bool                 `yaml:"required,omitempty"`
	Description string               `yaml:"description,omitempty"`
	Content     map[string]MediaType `yaml:"content"`
}

type Response struct {
	Description string               `yaml:"description"`
	Headers     map[string]Header    `yaml:"headers,omitempty"`
	Content     map[string]MediaType `yaml:"content,omitempty"`
	Ref         string               `yaml:"$ref,omitempty"`
}

type Header struct {
	Description string    `yaml:"description,omitempty"`
	Schema      *Property `yaml:"schema,omitempty"`
}

type MediaType struct {
	Schema *Property `yaml:"schema,omitempty"`
}

type Components struct {
	SecuritySchemes map[string]SecurityScheme `yaml:"securitySchemes"`
	Schemas         map[string]*Schema        `yaml:"schemas"`
	Parameters      map[string]Parameter      `yaml:"parameters,omitempty"`
	Responses       map[string]Response       `yaml:"responses,omitempty"`
}

type SecurityScheme struct {
	Type         string `yaml:"type"`
	Scheme       string `yaml:"scheme,omitempty"`
	BearerFormat string `yaml:"bearerFormat,omitempty"`
	In           string `yaml:"in,omitempty"`
	Name         string `yaml:"name,omitempty"`
	Description  string `yaml:"description,omitempty"`
}

// EndpointDef is used by the typed registry to declare an API endpoint
type EndpointDef struct {
	Method       string
	Path         string
	Tags         []string
	OperationID  string
	Summary      string
	Description  string
	Auth         string // "none", "bearer", "cookie", "both", "flexible"
	PathParams   []ParamDef
	QueryParams  []ParamDef
	HeaderParams []ParamDef
	RequestBody  any    // sample instance or nil
	ContentType  string // "application/json" (default), "multipart/form-data"
	SuccessCode  int    // e.g. 200, 201, 204
	ResponseBody any    // sample instance or nil
	Responses    map[string]Response
}

type ParamDef struct {
	Name        string
	Type        string // "string", "integer", "boolean"
	Format      string // "uuid", "int64"
	Required    bool
	Description string
	Example     any
}
