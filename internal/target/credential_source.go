// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package target

import (
	"context"

	"github.com/opendumpling/opendumpling/internal/credential"
	"github.com/opendumpling/opendumpling/internal/errors"
	"github.com/opendumpling/opendumpling/internal/target/store"
	"github.com/opendumpling/opendumpling/internal/types/subtypes"
)

// CredentialSourceType is the type of credential source.
type CredentialSourceType string

// Credential source type values.
const (
	LibraryCredentialSourceType CredentialSourceType = "library"
	StaticCredentialSourceType  CredentialSourceType = "static"
)

var _ CredentialSource = (*TargetCredentialSource)(nil)

// CredentialSource is an interface that can be implemented by both a library
// and a singular credential.
type CredentialSource interface {
	CredentialStoreId() string
	Id() string
	CredentialPurpose() credential.Purpose
	TargetId() string
	Type() CredentialSourceType
}

// CredentialSources contains slices of credential publicIds
// per purpose to be attached to the target.
type CredentialSources struct {
	BrokeredCredentialIds            []string
	InjectedApplicationCredentialIds []string
}

// A TargetCredentialSource represents the relationship between a target and a
// credential library and includes the id of the credential store that the
// library is a part of and the library's name and description.
//
// It implements the target.CredentialSource interface.
type TargetCredentialSource struct {
	*store.CredentialSource
	StoreId string
}

// TableName returns the tablename to override the default gorm table name
func (ts *TargetCredentialSource) TableName() string {
	return "target_credential_source"
}

// Id returns the ID of the credential source
func (ts *TargetCredentialSource) Id() string {
	return ts.CredentialSourceId
}

// CredentialStoreId returns the ID of the store containing the credential source
func (ts *TargetCredentialSource) CredentialStoreId() string {
	return ts.StoreId
}

// CredentialPurpose returns the purpose of the credential
func (ts *TargetCredentialSource) CredentialPurpose() credential.Purpose {
	return credential.Purpose(ts.GetCredentialPurpose())
}

// TargetId returns the target linked to this credential source
func (ts *TargetCredentialSource) TargetId() string {
	return ts.GetTargetId()
}

// Type returns the type of the credential source (library or static)
func (ts *TargetCredentialSource) Type() CredentialSourceType {
	return CredentialSourceType(ts.GetType())
}

// credentialSourceView provides a common way to return credential sources regardless of their
// underlying type (library or static).
type credentialSourceView struct {
	*store.CredentialSourceView
	tableName string `gorm:"-"`
}

// TableName returns the tablename to override the default gorm table name
func (csv *credentialSourceView) TableName() string {
	return "credential_source_all_types"
}

// VetCredentialSources checks that the provided credential sources are valid
// for the provided target subtype. It is used to (re)validate the credential
// sources attached to a target, for example when authorizing a session.
func VetCredentialSources(ctx context.Context, s subtypes.Subtype, sources []CredentialSource) error {
	const op = "target.VetCredentialSources"
	vet, ok := subtypeRegistry.vetCredentialSourcesFunc(s)
	if !ok {
		return errors.New(ctx, errors.InvalidParameter, op, "unsupported subtype")
	}
	libs := make([]*CredentialLibrary, 0, len(sources))
	creds := make([]*StaticCredential, 0, len(sources))
	for _, src := range sources {
		if src == nil {
			continue
		}
		switch src.Type() {
		case LibraryCredentialSourceType:
			libs = append(libs, &CredentialLibrary{CredentialLibrary: &store.CredentialLibrary{
				CredentialLibraryId: src.Id(),
				TargetId:            src.TargetId(),
				CredentialPurpose:   string(src.CredentialPurpose()),
			}})
		case StaticCredentialSourceType:
			creds = append(creds, &StaticCredential{StaticCredential: &store.StaticCredential{
				CredentialId:      src.Id(),
				TargetId:          src.TargetId(),
				CredentialPurpose: string(src.CredentialPurpose()),
			}})
		}
	}
	return vet(ctx, libs, creds)
}
