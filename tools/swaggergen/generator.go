package main

import (
	"fmt"
	"strings"
)

func BuildOpenAPIDoc(endpoints []EndpointDef, reflector *Reflector, sourceSpec *OpenAPIDoc) (*OpenAPIDoc, error) {
	doc := &OpenAPIDoc{
		OpenAPI: "3.0.3",
		Info: Info{
			Title:   "Waveyard API",
			Version: "1.0.0",
			Description: "HTTP API for the Waveyard beat marketplace.\n\n" +
				"Use **Authorize** to provide an access token as `Bearer <token>` for protected " +
				"operations. Refresh tokens are stored in an HTTP-only `refresh_token` cookie.",
			Contact: Contact{Name: "Waveyard Engineering"},
		},
		Servers: []Server{
			{URL: "/", Description: "Current API host"},
		},
		Security: []map[string][]string{},
		Tags: []Tag{
			{Name: "Operations", Description: "Health and observability endpoints"},
			{Name: "Authentication", Description: "Registration, sessions, verification, and password recovery"},
			{Name: "Catalog", Description: "Public beat catalog and producer catalog operations"},
			{Name: "Uploads", Description: "Durable multi-step producer upload pipeline"},
			{Name: "Users", Description: "User profiles and public producer information"},
			{Name: "Payments", Description: "Orders, payment verification, licenses, and downloads"},
			{Name: "Notifications", Description: "User notifications and realtime subscription"},
			{Name: "Analytics", Description: "Plays, favorites, and producer analytics"},
			{Name: "Admin", Description: "Super-admin management and audit operations"},
		},
		Paths: make(map[string]PathItem),
		Components: Components{
			SecuritySchemes: map[string]SecurityScheme{
				"bearerAuth": {
					Type:         "http",
					Scheme:       "bearer",
					BearerFormat: "JWT",
					Description:  "Provide the JWT access token returned by `/login` or `/auth/refresh` as `Bearer <token>`.",
				},
				"refreshCookie": {
					Type:        "apiKey",
					In:          "cookie",
					Name:        "refresh_token",
					Description: "HTTP-only cookie managed automatically by the browser across session endpoints.",
				},
			},
			Schemas: reflector.Schemas,
			Parameters: map[string]Parameter{
				"ID": {
					Name:        "id",
					In:          "path",
					Required:    true,
					Description: "Primary entity identifier (UUID or semantic slug/short code where supported)",
					Schema:      &Property{Type: "string"},
				},
				"AssetID": {
					Name:        "assetID",
					In:          "path",
					Required:    true,
					Description: "Asset identifier within a parent upload session",
					Schema:      &Property{Type: "string", Format: "uuid"},
				},
				"Page": {
					Name:        "page",
					In:          "query",
					Description: "1-based page number",
					Schema:      &Property{Type: "integer", Example: 1},
				},
				"Limit": {
					Name:        "limit",
					In:          "query",
					Description: "Maximum number of items per page (bounded by backend defaults)",
					Schema:      &Property{Type: "integer", Example: 20},
				},
			},
			Responses: map[string]Response{
				"SuccessMessage": {
					Description: "Standard operation acknowledgment",
					Content: map[string]MediaType{
						"application/json": {
							Schema: &Property{Ref: "#/components/schemas/SuccessMessageResponse"},
						},
					},
				},
				"BadRequest": {
					Description: "The request payload or parameters failed validation",
					Content: map[string]MediaType{
						"application/json": {
							Schema: &Property{Ref: "#/components/schemas/ErrorResponse"},
						},
					},
				},
				"Unauthorized": {
					Description: "Caller must authenticate to perform this action",
					Content: map[string]MediaType{
						"application/json": {
							Schema: &Property{Ref: "#/components/schemas/ErrorResponse"},
						},
					},
				},
				"Forbidden": {
					Description: "Caller lacks sufficient role or permission",
					Content: map[string]MediaType{
						"application/json": {
							Schema: &Property{Ref: "#/components/schemas/ErrorResponse"},
						},
					},
				},
				"NotFound": {
					Description: "Target resource was not found",
					Content: map[string]MediaType{
						"application/json": {
							Schema: &Property{Ref: "#/components/schemas/ErrorResponse"},
						},
					},
				},
				"Conflict": {
					Description: "Resource already exists or requested mutation conflicts with current state",
					Content: map[string]MediaType{
						"application/json": {
							Schema: &Property{Ref: "#/components/schemas/ErrorResponse"},
						},
					},
				},
				"InternalError": {
					Description: "Unexpected server-side error",
					Content: map[string]MediaType{
						"application/json": {
							Schema: &Property{Ref: "#/components/schemas/ErrorResponse"},
						},
					},
				},
			},
		},
	}

	// Register ErrorResponse schema if not present
	reflector.SetRawSchema("ErrorResponse", &Schema{
		Type:     "object",
		Required: []string{"error"},
		Properties: map[string]*Property{
			"error":   {Type: "string", Description: "Human readable error description"},
			"details": {Type: "string", Description: "Optional technical details", Nullable: true},
		},
	})

	// If sourceSpec has existing components/schemas (like FavoritePage, Admin types, etc.), preserve them!
	if sourceSpec != nil && sourceSpec.Components.Schemas != nil {
		for k, v := range sourceSpec.Components.Schemas {
			if _, exists := doc.Components.Schemas[k]; !exists {
				doc.Components.Schemas[k] = v
			}
		}
	}

	// Convert endpoints to OpenAPI path operations
	for _, ep := range endpoints {
		pathItem, exists := doc.Paths[ep.Path]
		if !exists {
			pathItem = make(PathItem)
			doc.Paths[ep.Path] = pathItem
		}

		op := &Operation{
			Tags:        ep.Tags,
			OperationID: ep.OperationID,
			Summary:     ep.Summary,
			Description: ep.Description,
			Responses:   make(map[string]Response),
		}

		// Security configuration
		switch ep.Auth {
		case "bearer":
			op.Security = []map[string][]string{{"bearerAuth": {}}}
		case "cookie":
			op.Security = []map[string][]string{{"refreshCookie": {}}}
		case "both":
			op.Security = []map[string][]string{{"bearerAuth": {}}, {"refreshCookie": {}}}
		case "flexible":
			op.Security = []map[string][]string{{}, {"bearerAuth": {}}}
		}

		// Parameters
		for _, p := range ep.PathParams {
			prop := &Property{Type: p.Type}
			if p.Format != "" {
				prop.Format = p.Format
			}
			op.Parameters = append(op.Parameters, Parameter{
				Name:        p.Name,
				In:          "path",
				Required:    true,
				Description: p.Description,
				Schema:      prop,
			})
		}
		for _, q := range ep.QueryParams {
			prop := &Property{Type: q.Type}
			if q.Format != "" {
				prop.Format = q.Format
			}
			op.Parameters = append(op.Parameters, Parameter{
				Name:        q.Name,
				In:          "query",
				Required:    q.Required,
				Description: q.Description,
				Schema:      prop,
			})
		}

		for _, h := range ep.HeaderParams {
			op.Parameters = append(op.Parameters, Parameter{
				Name: h.Name, In: "header", Required: h.Required,
				Description: h.Description, Schema: &Property{Type: h.Type},
			})
		}

		// Request Body
		if ep.RequestBody != nil {
			cType := ep.ContentType
			if cType == "" {
				cType = "application/json"
			}

			var bodyProp *Property
			if cType == "application/json" {
				bodyProp = reflector.GenerateProperty(ep.RequestBody)
			} else {
				// Form/multipart
				bodyProp = &Property{Type: "object"}
			}

			op.RequestBody = &RequestBody{
				Required: true,
				Content: map[string]MediaType{
					cType: {Schema: bodyProp},
				},
			}
		}

		// Responses
		successCodeStr := fmt.Sprintf("%d", ep.SuccessCode)
		if ep.SuccessCode == 0 {
			successCodeStr = "200"
		}

		var respProp *Property
		if ep.ResponseBody != nil {
			respProp = reflector.GenerateProperty(ep.ResponseBody)
		}

		if respProp != nil {
			op.Responses[successCodeStr] = Response{
				Description: "Operation successful",
				Content: map[string]MediaType{
					"application/json": {Schema: respProp},
				},
			}
		} else {
			switch ep.SuccessCode {
			case 204:
				op.Responses["204"] = Response{Description: "No content"}
			case 410:
				op.Responses["410"] = Response{Description: "Gone: Endpoint discontinued"}
			case 101:
				op.Responses["101"] = Response{Description: "Switching Protocols (WebSocket upgrade)"}
			default:
				op.Responses[successCodeStr] = Response{Description: "OK"}
			}
		}

		// Standard error references
		op.Responses["400"] = Response{Ref: "#/components/responses/BadRequest"}
		if ep.Auth == "bearer" || ep.Auth == "flexible" {
			op.Responses["401"] = Response{Ref: "#/components/responses/Unauthorized"}
			op.Responses["403"] = Response{Ref: "#/components/responses/Forbidden"}
		}
		op.Responses["500"] = Response{Ref: "#/components/responses/InternalError"}

		for code, response := range ep.Responses {
			op.Responses[code] = response
		}

		methodLower := strings.ToLower(ep.Method)
		pathItem[methodLower] = op
	}

	// Also merge any existing custom endpoints from sourceSpec if they weren't covered yet (e.g. detailed admin docs)
	if sourceSpec != nil {
		for path, pathItem := range sourceSpec.Paths {
			if _, exists := doc.Paths[path]; !exists {
				doc.Paths[path] = pathItem
			} else {
				for method, op := range pathItem {
					if _, methExists := doc.Paths[path][method]; !methExists {
						doc.Paths[path][method] = op
					}
				}
			}
		}
	}

	return doc, nil
}
