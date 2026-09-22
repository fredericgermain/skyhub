package main

import (
	"context"
	"net/http"
	"net/url"
)

type formKey struct{}

func contextWithForm(r *http.Request, form url.Values) context.Context {
	return context.WithValue(r.Context(), formKey{}, form)
}
