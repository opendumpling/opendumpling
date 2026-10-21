// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package aliases

import (
	"context"
	"fmt"
	"strings"

	"github.com/opendumpling/opendumpling/globals"
	aliasRepo "github.com/opendumpling/opendumpling/internal/alias"
	"github.com/opendumpling/opendumpling/internal/alias/store"
	"github.com/opendumpling/opendumpling/internal/daemon/controller/auth"
	"github.com/opendumpling/opendumpling/internal/daemon/controller/common"
	"github.com/opendumpling/opendumpling/internal/daemon/controller/common/scopeids"
	"github.com/opendumpling/opendumpling/internal/daemon/controller/handlers"
	"github.com/opendumpling/opendumpling/internal/errors"
	pbs "github.com/opendumpling/opendumpling/internal/gen/controller/api/services"
	"github.com/opendumpling/opendumpling/internal/perms"
	"github.com/opendumpling/opendumpling/internal/requests"
	"github.com/opendumpling/opendumpling/internal/types/action"
	"github.com/opendumpling/opendumpling/internal/types/resource"
	"github.com/opendumpling/opendumpling/internal/types/scope"
	pb "github.com/opendumpling/opendumpling/sdk/pbs/controller/api/resources/aliases"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

var (
	maskManager handlers.MaskManager

	// IdActions contains the set of actions that can be performed on individual
	// aliases.
	IdActions = action.ActionSet{
		action.NoOp,
		action.Read,
		action.Update,
		action.Delete,
	}

	// CollectionActions contains the set of actions that can be performed on
	// the alias collection.
	CollectionActions = action.ActionSet{
		action.Create,
		action.List,
	}
)

func init() {
	var err error
	if maskManager, err = handlers.NewMaskManager(
		context.Background(),
		handlers.MaskDestination{&store.Alias{}},
		handlers.MaskSource{&pb.Alias{}},
	); err != nil {
		panic(err)
	}
}

// Service handles requests as described by the pbs.AliasServiceServer interface.
type Service struct {
	pbs.UnsafeAliasServiceServer

	repoFn    common.AliasRepoFactory
	iamRepoFn common.IamRepoFactory
}

var _ pbs.AliasServiceServer = (*Service)(nil)

// NewService returns an alias service which handles alias related requests.
func NewService(ctx context.Context, repo common.AliasRepoFactory, iamRepo common.IamRepoFactory) (Service, error) {
	const op = "aliases.NewService"
	if repo == nil {
		return Service{}, errors.New(ctx, errors.InvalidParameter, op, "missing alias repository")
	}
	if iamRepo == nil {
		return Service{}, errors.New(ctx, errors.InvalidParameter, op, "missing iam repository")
	}
	return Service{repoFn: repo, iamRepoFn: iamRepo}, nil
}

// ListAliases implements the interface pbs.AliasServiceServer.
func (s Service) ListAliases(ctx context.Context, req *pbs.ListAliasesRequest) (*pbs.ListAliasesResponse, error) {
	if err := validateListRequest(ctx, req); err != nil {
		return nil, err
	}
	authResults := s.authResult(ctx, req.GetScopeId(), action.List)
	if authResults.Error != nil {
		if (authResults.Error == handlers.ForbiddenError() || authResults.Error == handlers.UnauthenticatedError()) &&
			req.GetRecursive() &&
			authResults.AuthenticationFinished {
		} else {
			return nil, authResults.Error
		}
	}

	scopeIds, scopeInfoMap, err := scopeids.GetListingScopeIds(
		ctx, s.iamRepoFn, authResults, req.GetScopeId(), resource.Alias, req.GetRecursive())
	if err != nil {
		return nil, err
	}
	// If no scopes match, return an empty response
	if len(scopeIds) == 0 {
		return &pbs.ListAliasesResponse{}, nil
	}

	al, err := s.listFromRepo(ctx, scopeIds)
	if err != nil {
		return nil, err
	}
	if len(al) == 0 {
		return &pbs.ListAliasesResponse{}, nil
	}

	filter, err := handlers.NewFilter(ctx, req.GetFilter())
	if err != nil {
		return nil, err
	}
	finalItems := make([]*pb.Alias, 0, len(al))
	res := perms.Resource{
		Type: resource.Alias,
	}
	for _, item := range al {
		res.Id = item.GetPublicId()
		res.ScopeId = item.GetScopeId()
		authorizedActions := authResults.FetchActionSetForId(ctx, item.GetPublicId(), IdActions, auth.WithResource(&res)).Strings()
		if len(authorizedActions) == 0 {
			continue
		}

		outputFields := authResults.FetchOutputFields(res, action.List).SelfOrDefaults(authResults.UserId)
		outputOpts := make([]handlers.Option, 0, 3)
		outputOpts = append(outputOpts, handlers.WithOutputFields(outputFields))
		if outputFields.Has(globals.ScopeField) {
			outputOpts = append(outputOpts, handlers.WithScope(scopeInfoMap[item.GetScopeId()]))
		}
		if outputFields.Has(globals.AuthorizedActionsField) {
			outputOpts = append(outputOpts, handlers.WithAuthorizedActions(authorizedActions))
		}

		item, err := toProto(ctx, item, outputOpts...)
		if err != nil {
			return nil, err
		}

		if filter.Match(item) {
			finalItems = append(finalItems, item)
		}
	}
	return &pbs.ListAliasesResponse{Items: finalItems}, nil
}

// GetAlias implements the interface pbs.AliasServiceServer.
func (s Service) GetAlias(ctx context.Context, req *pbs.GetAliasRequest) (*pbs.GetAliasResponse, error) {
	const op = "aliases.(Service).GetAlias"

	if err := validateGetRequest(req); err != nil {
		return nil, err
	}
	authResults := s.authResult(ctx, req.GetId(), action.Read)
	if authResults.Error != nil {
		return nil, authResults.Error
	}
	a, err := s.getFromRepo(ctx, req.GetId())
	if err != nil {
		return nil, err
	}

	outputFields, ok := requests.OutputFields(ctx)
	if !ok {
		return nil, errors.New(ctx, errors.Internal, op, "no request context found")
	}

	outputOpts := make([]handlers.Option, 0, 3)
	outputOpts = append(outputOpts, handlers.WithOutputFields(outputFields))
	if outputFields.Has(globals.ScopeField) {
		outputOpts = append(outputOpts, handlers.WithScope(authResults.Scope))
	}
	if outputFields.Has(globals.AuthorizedActionsField) {
		outputOpts = append(outputOpts, handlers.WithAuthorizedActions(authResults.FetchActionSetForId(ctx, a.GetPublicId(), IdActions).Strings()))
	}

	item, err := toProto(ctx, a, outputOpts...)
	if err != nil {
		return nil, err
	}

	return &pbs.GetAliasResponse{Item: item}, nil
}

// CreateAlias implements the interface pbs.AliasServiceServer.
func (s Service) CreateAlias(ctx context.Context, req *pbs.CreateAliasRequest) (*pbs.CreateAliasResponse, error) {
	const op = "aliases.(Service).CreateAlias"

	if err := validateCreateRequest(req); err != nil {
		return nil, err
	}
	authResults := s.authResult(ctx, req.GetItem().GetScopeId(), action.Create)
	if authResults.Error != nil {
		return nil, authResults.Error
	}
	a, err := s.createInRepo(ctx, authResults.Scope.GetId(), req.GetItem())
	if err != nil {
		return nil, err
	}

	outputFields, ok := requests.OutputFields(ctx)
	if !ok {
		return nil, errors.New(ctx, errors.Internal, op, "no request context found")
	}

	outputOpts := make([]handlers.Option, 0, 3)
	outputOpts = append(outputOpts, handlers.WithOutputFields(outputFields))
	if outputFields.Has(globals.ScopeField) {
		outputOpts = append(outputOpts, handlers.WithScope(authResults.Scope))
	}
	if outputFields.Has(globals.AuthorizedActionsField) {
		outputOpts = append(outputOpts, handlers.WithAuthorizedActions(authResults.FetchActionSetForId(ctx, a.GetPublicId(), IdActions).Strings()))
	}

	item, err := toProto(ctx, a, outputOpts...)
	if err != nil {
		return nil, err
	}

	return &pbs.CreateAliasResponse{Item: item, Uri: fmt.Sprintf("aliases/%s", item.GetId())}, nil
}

// UpdateAlias implements the interface pbs.AliasServiceServer.
func (s Service) UpdateAlias(ctx context.Context, req *pbs.UpdateAliasRequest) (*pbs.UpdateAliasResponse, error) {
	const op = "aliases.(Service).UpdateAlias"

	if err := validateUpdateRequest(req); err != nil {
		return nil, err
	}
	authResults := s.authResult(ctx, req.GetId(), action.Update)
	if authResults.Error != nil {
		return nil, authResults.Error
	}
	a, err := s.updateInRepo(ctx, authResults.Scope.GetId(), req.GetId(), req.GetUpdateMask().GetPaths(), req.GetItem())
	if err != nil {
		return nil, err
	}

	outputFields, ok := requests.OutputFields(ctx)
	if !ok {
		return nil, errors.New(ctx, errors.Internal, op, "no request context found")
	}

	outputOpts := make([]handlers.Option, 0, 3)
	outputOpts = append(outputOpts, handlers.WithOutputFields(outputFields))
	if outputFields.Has(globals.ScopeField) {
		outputOpts = append(outputOpts, handlers.WithScope(authResults.Scope))
	}
	if outputFields.Has(globals.AuthorizedActionsField) {
		outputOpts = append(outputOpts, handlers.WithAuthorizedActions(authResults.FetchActionSetForId(ctx, a.GetPublicId(), IdActions).Strings()))
	}

	item, err := toProto(ctx, a, outputOpts...)
	if err != nil {
		return nil, err
	}

	return &pbs.UpdateAliasResponse{Item: item}, nil
}

// DeleteAlias implements the interface pbs.AliasServiceServer.
func (s Service) DeleteAlias(ctx context.Context, req *pbs.DeleteAliasRequest) (*pbs.DeleteAliasResponse, error) {
	if err := validateDeleteRequest(req); err != nil {
		return nil, err
	}
	authResults := s.authResult(ctx, req.GetId(), action.Delete)
	if authResults.Error != nil {
		return nil, authResults.Error
	}
	_, err := s.deleteFromRepo(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func (s Service) getFromRepo(ctx context.Context, id string) (*aliasRepo.Alias, error) {
	const op = "aliases.(Service).getFromRepo"
	repo, err := s.repoFn()
	if err != nil {
		return nil, errors.Wrap(ctx, err, op)
	}
	a, err := repo.LookupAlias(ctx, id)
	if err != nil && !errors.IsNotFoundError(err) {
		return nil, errors.Wrap(ctx, err, op)
	}
	if a == nil {
		return nil, handlers.NotFoundErrorf("Alias %q doesn't exist.", id)
	}
	return a, nil
}

func (s Service) createInRepo(ctx context.Context, scopeId string, item *pb.Alias) (*aliasRepo.Alias, error) {
	const op = "aliases.(Service).createInRepo"
	var opts []aliasRepo.Option
	if item.GetValue() != nil {
		opts = append(opts, aliasRepo.WithValue(item.GetValue().GetValue()))
	}
	if item.GetName() != nil {
		opts = append(opts, aliasRepo.WithName(item.GetName().GetValue()))
	}
	if item.GetDescription() != nil {
		opts = append(opts, aliasRepo.WithDescription(item.GetDescription().GetValue()))
	}
	if item.GetDestinationId() != nil {
		opts = append(opts, aliasRepo.WithDestinationId(item.GetDestinationId().GetValue()))
	}
	a, err := aliasRepo.NewAlias(ctx, scopeId, opts...)
	if err != nil {
		return nil, handlers.ApiErrorWithCodeAndMessage(codes.Internal, "Unable to build alias for creation: %v.", err)
	}
	repo, err := s.repoFn()
	if err != nil {
		return nil, errors.Wrap(ctx, err, op)
	}
	out, err := repo.CreateAlias(ctx, a)
	if err != nil {
		return nil, errors.Wrap(ctx, err, op)
	}
	if out == nil {
		return nil, handlers.ApiErrorWithCodeAndMessage(codes.Internal, "Unable to create alias but no error returned from repository.")
	}
	return out, nil
}

func (s Service) updateInRepo(ctx context.Context, scopeId, id string, mask []string, item *pb.Alias) (*aliasRepo.Alias, error) {
	const op = "aliases.(Service).updateInRepo"
	var opts []aliasRepo.Option
	if value := item.GetValue(); value != nil {
		opts = append(opts, aliasRepo.WithValue(value.GetValue()))
	}
	if name := item.GetName(); name != nil {
		opts = append(opts, aliasRepo.WithName(name.GetValue()))
	}
	if desc := item.GetDescription(); desc != nil {
		opts = append(opts, aliasRepo.WithDescription(desc.GetValue()))
	}
	if destination := item.GetDestinationId(); destination != nil {
		opts = append(opts, aliasRepo.WithDestinationId(destination.GetValue()))
	}
	version := item.GetVersion()
	a, err := aliasRepo.NewAlias(ctx, scopeId, opts...)
	if err != nil {
		return nil, handlers.ApiErrorWithCodeAndMessage(codes.Internal, "Unable to build alias for update: %v.", err)
	}
	a.PublicId = id
	dbMask := maskManager.Translate(mask)
	if len(dbMask) == 0 {
		return nil, handlers.InvalidArgumentErrorf("No valid fields included in the update mask.", map[string]string{"update_mask": "No valid fields provided in the update mask."})
	}
	repo, err := s.repoFn()
	if err != nil {
		return nil, errors.Wrap(ctx, err, op)
	}
	out, rowsUpdated, err := repo.UpdateAlias(ctx, a, version, dbMask)
	if err != nil {
		return nil, errors.Wrap(ctx, err, op)
	}
	if rowsUpdated == 0 {
		return nil, handlers.NotFoundErrorf("Alias %q doesn't exist or incorrect version provided.", id)
	}
	return out, nil
}

func (s Service) deleteFromRepo(ctx context.Context, id string) (bool, error) {
	const op = "aliases.(Service).deleteFromRepo"
	repo, err := s.repoFn()
	if err != nil {
		return false, err
	}
	rows, err := repo.DeleteAlias(ctx, id)
	if err != nil {
		if errors.IsNotFoundError(err) {
			return false, nil
		}
		return false, errors.Wrap(ctx, err, op, errors.WithMsg("unable to delete alias"))
	}
	return rows > 0, nil
}

func (s Service) listFromRepo(ctx context.Context, scopeIds []string) ([]*aliasRepo.Alias, error) {
	const op = "aliases.(Service).listFromRepo"
	repo, err := s.repoFn()
	if err != nil {
		return nil, errors.Wrap(ctx, err, op)
	}
	al, err := repo.ListAliases(ctx, scopeIds, aliasRepo.WithLimit(-1))
	if err != nil {
		return nil, errors.Wrap(ctx, err, op)
	}
	return al, nil
}

func (s Service) authResult(ctx context.Context, id string, a action.Type) auth.VerifyResults {
	res := auth.VerifyResults{}
	repo, err := s.repoFn()
	if err != nil {
		res.Error = err
		return res
	}
	iamRepo, err := s.iamRepoFn()
	if err != nil {
		res.Error = err
		return res
	}

	var parentId string
	opts := []auth.Option{auth.WithType(resource.Alias), auth.WithAction(a)}
	switch a {
	case action.List, action.Create:
		parentId = id
		scp, err := iamRepo.LookupScope(ctx, parentId)
		if err != nil {
			res.Error = err
			return res
		}
		if scp == nil {
			res.Error = handlers.NotFoundError()
			return res
		}
	default:
		al, err := repo.LookupAlias(ctx, id)
		if err != nil {
			res.Error = err
			return res
		}
		if al == nil {
			res.Error = handlers.NotFoundError()
			return res
		}
		parentId = al.GetScopeId()
		opts = append(opts, auth.WithId(id))
	}
	opts = append(opts, auth.WithScopeId(parentId))
	return auth.Verify(ctx, opts...)
}

func toProto(ctx context.Context, in *aliasRepo.Alias, opt ...handlers.Option) (*pb.Alias, error) {
	opts := handlers.GetOpts(opt...)
	if opts.WithOutputFields == nil {
		return nil, handlers.ApiErrorWithCodeAndMessage(codes.Internal, "output fields not found when building alias proto")
	}
	outputFields := *opts.WithOutputFields

	out := pb.Alias{}
	if outputFields.Has(globals.IdField) {
		out.Id = in.GetPublicId()
	}
	if outputFields.Has(globals.ScopeIdField) {
		out.ScopeId = in.GetScopeId()
	}
	if outputFields.Has(globals.NameField) && in.GetName() != "" {
		out.Name = wrapperspb.String(in.GetName())
	}
	if outputFields.Has(globals.ValueField) && in.GetValue() != "" {
		out.Value = wrapperspb.String(in.GetValue())
	}
	if outputFields.Has(globals.DescriptionField) && in.GetDescription() != "" {
		out.Description = wrapperspb.String(in.GetDescription())
	}
	if outputFields.Has(globals.DestinationIdField) && in.GetDestinationId() != "" {
		out.DestinationId = wrapperspb.String(in.GetDestinationId())
		if outputFields.Has(globals.DestinationTypeField) {
			out.DestinationType = destinationTypeFromId(in.GetDestinationId())
		}
	}
	if outputFields.Has(globals.CreatedTimeField) {
		out.CreatedTime = in.GetCreateTime().GetTimestamp()
	}
	if outputFields.Has(globals.UpdatedTimeField) {
		out.UpdatedTime = in.GetUpdateTime().GetTimestamp()
	}
	if outputFields.Has(globals.VersionField) {
		out.Version = in.GetVersion()
	}
	if outputFields.Has(globals.ScopeField) {
		out.Scope = opts.WithScope
	}
	if outputFields.Has(globals.AuthorizedActionsField) {
		out.AuthorizedActions = opts.WithAuthorizedActions
	}
	return &out, nil
}

// destinationTypeFromId derives the resource type of a destination from its
// public id prefix. An unknown prefix yields "unknown".
func destinationTypeFromId(id string) string {
	t := globals.ResourceTypeFromPrefix(id)
	if t == resource.Unknown {
		return "unknown"
	}
	return t.String()
}

// A validateX method should exist for each method above. These methods do not
// make calls to any backing service but enforce requirements on the structure
// of the request.
func validateGetRequest(req *pbs.GetAliasRequest) error {
	return handlers.ValidateGetRequest(handlers.NoopValidatorFn, req, globals.AliasPrefix)
}

func validateCreateRequest(req *pbs.CreateAliasRequest) error {
	return handlers.ValidateCreateRequest(req.GetItem(), func() map[string]string {
		badFields := map[string]string{}
		if !handlers.ValidId(handlers.Id(req.GetItem().GetScopeId()), scope.Org.Prefix()) &&
			!handlers.ValidId(handlers.Id(req.GetItem().GetScopeId()), scope.Project.Prefix()) &&
			scope.Global.String() != req.GetItem().GetScopeId() {
			badFields["scope_id"] = "This field is missing or improperly formatted."
		}
		if req.GetItem().GetValue() == nil || req.GetItem().GetValue().GetValue() == "" {
			badFields[globals.ValueField] = "This field is required."
		} else if err := validateAliasValue(req.GetItem().GetValue().GetValue()); err != nil {
			badFields[globals.ValueField] = err.Error()
		}
		if req.GetItem().GetDestinationId() == nil || req.GetItem().GetDestinationId().GetValue() == "" {
			badFields[globals.DestinationIdField] = "This field is required."
		} else if !handlers.ValidId(handlers.Id(req.GetItem().GetDestinationId().GetValue()), globals.TcpTargetPrefix, globals.SshTargetPrefix) {
			badFields[globals.DestinationIdField] = "This field must be a target id."
		}
		return badFields
	})
}

func validateUpdateRequest(req *pbs.UpdateAliasRequest) error {
	return handlers.ValidateUpdateRequest(req, req.GetItem(), func() map[string]string {
		badFields := map[string]string{}
		if req.GetItem().GetValue() != nil {
			if req.GetItem().GetValue().GetValue() == "" {
				badFields[globals.ValueField] = "This field cannot be empty when provided."
			} else if err := validateAliasValue(req.GetItem().GetValue().GetValue()); err != nil {
				badFields[globals.ValueField] = err.Error()
			}
		}
		if req.GetItem().GetDestinationId() != nil &&
			!handlers.ValidId(handlers.Id(req.GetItem().GetDestinationId().GetValue()), globals.TcpTargetPrefix, globals.SshTargetPrefix) {
			badFields[globals.DestinationIdField] = "This field must be a target id."
		}
		return badFields
	}, globals.AliasPrefix)
}

func validateDeleteRequest(req *pbs.DeleteAliasRequest) error {
	return handlers.ValidateDeleteRequest(handlers.NoopValidatorFn, req, globals.AliasPrefix)
}

func validateListRequest(ctx context.Context, req *pbs.ListAliasesRequest) error {
	badFields := map[string]string{}
	if !handlers.ValidId(handlers.Id(req.GetScopeId()), scope.Org.Prefix()) &&
		!handlers.ValidId(handlers.Id(req.GetScopeId()), scope.Project.Prefix()) &&
		req.GetScopeId() != scope.Global.String() {
		badFields["scope_id"] = "Incorrectly formatted identifier."
	}
	if _, err := handlers.NewFilter(ctx, req.GetFilter()); err != nil {
		badFields["filter"] = fmt.Sprintf("This field could not be parsed. %v", err)
	}
	if len(badFields) > 0 {
		return handlers.InvalidArgumentErrorf("Improperly formatted identifier.", badFields)
	}
	return nil
}

// validateAliasValue enforces the shape of an alias value: it must be a
// DNS-like label so it survives being typed after `ssh ` without quoting.
func validateAliasValue(value string) error {
	if len(value) > 253 {
		return errors.New(context.Background(), errors.InvalidParameter, "aliases.validateAliasValue", "value must be 253 characters or fewer")
	}
	if strings.ContainsAny(value, " \t\r\n") {
		return errors.New(context.Background(), errors.InvalidParameter, "aliases.validateAliasValue", "value must not contain whitespace")
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '-' || r == '_':
		default:
			return errors.New(context.Background(), errors.InvalidParameter, "aliases.validateAliasValue", "value may only contain letters, digits, dots, dashes and underscores")
		}
	}
	return nil
}
