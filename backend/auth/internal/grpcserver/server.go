// Package grpcserver is the auth.v1 transport of the auth service: it turns the answers of the
// case layer into status codes and back, and owns nothing but that translation.
package grpcserver

import (
	"context"
	"errors"

	"github.com/Keane81/Cabby/backend/auth/internal/service"
	"github.com/Keane81/Cabby/backend/contracts/authpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server is the auth.v1 transport of the cases: all four methods of the contract are served, and
// each of them only calls its case and translates the answer.
type Server struct {
	authpb.UnimplementedAuthServiceServer
	service *service.Service
}

// NewServer binds the transport to the service layer.
func NewServer(svc *service.Service) *Server {
	return &Server{service: svc}
}

// Register attaches the service to a gRPC server.
func (s *Server) Register(grpcServer *grpc.Server) {
	authpb.RegisterAuthServiceServer(grpcServer, s)
}

// RegisterCabber creates an account and answers with what the cabber needs to know about it.
func (s *Server) RegisterCabber(
	ctx context.Context,
	req *authpb.RegisterCabberRequest,
) (*authpb.RegisterCabberResponse, error) {
	created, err := s.service.Register(ctx, req.GetName(), req.GetEmail(), req.GetPassword())
	if err != nil {
		return nil, toStatus(err)
	}
	return &authpb.RegisterCabberResponse{
		CabberId: created.ID,
		Email:    created.Email,
	}, nil
}

// CreateCabberSession checks the credentials and opens one session of its own for the caller.
func (s *Server) CreateCabberSession(
	ctx context.Context,
	req *authpb.CreateCabberSessionRequest,
) (*authpb.CreateCabberSessionResponse, error) {
	session, err := s.service.CreateSession(ctx, req.GetEmail(), req.GetPassword())
	if err != nil {
		return nil, toStatus(err)
	}
	return &authpb.CreateCabberSessionResponse{
		AccessToken:   session.Token,
		ExpiresAtUnix: session.ExpiresAt.Unix(),
	}, nil
}

// DeleteCabberSession revokes the session the caller presents. The credential is the only input:
// the request names no account, because the session is the account (R-03, FR-019).
func (s *Server) DeleteCabberSession(
	ctx context.Context,
	req *authpb.DeleteCabberSessionRequest,
) (*authpb.DeleteCabberSessionResponse, error) {
	if err := s.service.DeleteSession(ctx, req.GetAccessToken()); err != nil {
		return nil, toStatus(err)
	}
	return &authpb.DeleteCabberSessionResponse{}, nil
}

// VerifyCabberSession confirms the session the caller presents and names its owner. It revokes
// nothing: the owner it returns is what another service acts on (spec 004 FR-009), and the answer
// for an invalid session is the one DeleteCabberSession gives, so the two cannot be told apart.
func (s *Server) VerifyCabberSession(
	ctx context.Context,
	req *authpb.VerifyCabberSessionRequest,
) (*authpb.VerifyCabberSessionResponse, error) {
	owner, err := s.service.Verify(ctx, req.GetAccessToken())
	if err != nil {
		return nil, toStatus(err)
	}
	return &authpb.VerifyCabberSessionResponse{CabberId: owner}, nil
}

// Messages are fixed texts of the transport, never a cause: a database message can quote a row
// value, and neither a status text nor a log line may repeat one (FR-004, data-model §6).
const (
	msgInvalidField   = "auth: the request has an invalid field"
	msgEmailTaken     = "auth: the email is already registered"
	msgSessionInvalid = "auth: the credentials or the session are not valid"
	msgStorageGone    = "auth: the account storage is unavailable"
	msgInternal       = "auth: the request could not be completed"
)

// toStatus is the one mapping from a case failure to the status the contract defines. A failure
// the mapping does not know is INTERNAL with no detail: guessing at an unknown cause would be a
// report of the internals (FR-004).
func toStatus(err error) error {
	var defect service.Validation
	if errors.As(err, &defect) {
		// The second result only reports a detail that cannot be attached, and one ErrorField is
		// far from the limit; the status keeps its code and message either way.
		withField, _ := status.New(codes.InvalidArgument, msgInvalidField).
			WithDetails(&authpb.ErrorField{Field: string(defect.Field), Reason: string(defect.Reason)})
		return withField.Err()
	}
	switch {
	case errors.Is(err, service.ErrEmailTaken):
		return status.Error(codes.AlreadyExists, msgEmailTaken)
	case errors.Is(err, service.ErrInvalidSession):
		return status.Error(codes.Unauthenticated, msgSessionInvalid)
	case errors.Is(err, service.ErrDependency):
		return status.Error(codes.Unavailable, msgStorageGone)
	default:
		return status.Error(codes.Internal, msgInternal)
	}
}
