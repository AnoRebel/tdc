package httpapi

//go:generate go run ../../cmd/tdc-openapi-gen

import "reflect"

// routeBodySet is what one route's handler was found to bind and write; see
// internal/openapigen.
type routeBodySet struct {
	Request   reflect.Type
	Query     []string
	Responses []responseBody
	Raw       map[int]string
}

type responseBody struct {
	Status int
	Type   reflect.Type
}
