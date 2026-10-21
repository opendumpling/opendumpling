// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package alias

// getOpts - iterate the inbound Options and return a struct
func getOpts(opt ...Option) options {
	opts := getDefaultOptions()
	for _, o := range opt {
		o(&opts)
	}
	return opts
}

// Option - how Options are passed as arguments
type Option func(*options)

// options = how options are represented
type options struct {
	withPublicId      string
	withName          string
	withValue         string
	withDescription   string
	withDestinationId string
	withLimit         int
}

// WithName provides an optional name
func WithName(name string) Option {
	return func(o *options) {
		o.withName = name
	}
}

func getDefaultOptions() options {
	return options{}
}

// WithPublicId provides an optional public id
func WithPublicId(id string) Option {
	return func(o *options) {
		o.withPublicId = id
	}
}

// WithValue provides the alias value
func WithValue(value string) Option {
	return func(o *options) {
		o.withValue = value
	}
}

// WithDescription provides an optional description
func WithDescription(desc string) Option {
	return func(o *options) {
		o.withDescription = desc
	}
}

// WithDestinationId provides the destination the alias points at
func WithDestinationId(id string) Option {
	return func(o *options) {
		o.withDestinationId = id
	}
}

// WithLimit provides an optional limit for list operations
func WithLimit(limit int) Option {
	return func(o *options) {
		o.withLimit = limit
	}
}
