// Package grpcserver is the location.v1 transport of the location service: it turns the answers of
// the case layer into status codes and back, and owns nothing but that translation and what the
// transport observes (request logs and Prometheus metrics).
package grpcserver

import (
	"context"
	"errors"

	"github.com/Keane81/Cabby/backend/contracts/locationpb"
	"github.com/Keane81/Cabby/backend/location/internal/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server is the location.v1 transport of the cases.
type Server struct {
	locationpb.UnimplementedLocationServiceServer
	service *service.Service
}

// NewServer binds the transport to the service layer.
func NewServer(svc *service.Service) *Server {
	return &Server{service: svc}
}

// Register attaches the service to a gRPC server.
func (s *Server) Register(grpcServer *grpc.Server) {
	locationpb.RegisterLocationServiceServer(grpcServer, s)
}

// RecordCabberLocation adds one record of the position of the cabber the request names. The cabber
// is trusted: the service is not reachable from outside the compose network, and the gateway takes
// the identifier from a confirmed session (spec 004 FR-009).
func (s *Server) RecordCabberLocation(
	ctx context.Context,
	req *locationpb.RecordCabberLocationRequest,
) (*locationpb.RecordCabberLocationResponse, error) {
	receivedAt, err := s.service.Record(ctx, req.GetCabberId(), req.GetLatitude(), req.GetLongitude())
	if err != nil {
		return nil, toStatus(err)
	}
	return &locationpb.RecordCabberLocationResponse{
		ReceivedAtUnix:  receivedAt.Unix(),
		ReceivedAtNanos: int32(receivedAt.Nanosecond()),
	}, nil
}

// Messages are fixed texts of the transport, never a cause: a database message can quote a row
// value, and neither a status text nor a log line may repeat a coordinate (FR-011).
const (
	msgInvalidField = "location: the request has an invalid field"
	msgStorageGone  = "location: the location storage is unavailable"
	msgInternal     = "location: the request could not be completed"
)

// toStatus is the one mapping from a case failure to the status the contract defines. A failure
// the mapping does not know is INTERNAL with no detail.
func toStatus(err error) error {
	var defect service.Validation
	if errors.As(err, &defect) {
		// The second result only reports a detail that cannot be attached; the status keeps its
		// code and message either way.
		withField, _ := status.New(codes.InvalidArgument, msgInvalidField).
			WithDetails(&locationpb.ErrorField{Field: string(defect.Field), Reason: string(defect.Reason)})
		return withField.Err()
	}
	switch {
	case errors.Is(err, service.ErrDependency):
		return status.Error(codes.Unavailable, msgStorageGone)
	default:
		return status.Error(codes.Internal, msgInternal)
	}
}
