// Package locationclient is the gRPC transport of the location service behind the Locations port. It
// owns the shape of one call — its deadline and the translation of a gRPC status into a domain
// result — and no business rule: location decides those (spec 004 plan.md §Structure).
package locationclient

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Keane81/Cabby/backend/contracts/locationpb"
	"github.com/Keane81/Cabby/backend/platform/requestid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// CallTimeout bounds one call to the location service. A slow answer is reported as an unavailable
// dependency rather than kept waiting for.
const CallTimeout = 2 * time.Second

// Invalid reports the field a request was rejected on and why (FR-002). The value the client sent is
// never part of it (FR-011): it is a coordinate.
type Invalid struct {
	Field  string
	Reason string
}

func (i Invalid) Error() string {
	return fmt.Sprintf("locationclient: field %s is %s", i.Field, i.Reason)
}

// Failures of the dependency, kept apart from a rejection by their identity so the server can map
// them without reading a message.
var (
	ErrUnavailable = errors.New("locationclient: location service is unavailable")
	ErrInternal    = errors.New("locationclient: location service failed")
)

// Locations is the port the public server drives. The fake of the tests and the gRPC client below
// implement the same method.
type Locations interface {
	// RecordCabberLocation adds one record of the position of a cabber and returns the time the
	// service received it.
	RecordCabberLocation(ctx context.Context, cabberID string, latitude, longitude float64) (time.Time, error)
}

// Client talks to location.v1.LocationService over one connection. grpc-go dials lazily and retries
// nothing by default, which is exactly the policy of spec 004 R-06: a call is made once or not at all,
// because a repeat of a call that did commit would add a second record the client never asked for.
type Client struct {
	conn    *grpc.ClientConn
	api     locationpb.LocationServiceClient
	timeout time.Duration
}

// Dial prepares the client for the address of CABBY_LOCATION_ADDR. It does not connect, so a gateway
// that starts without location reachable still serves /healthz.
func Dial(address string) (*Client, error) {
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("locationclient: cannot dial %s: %w", address, err)
	}
	return &Client{conn: conn, api: locationpb.NewLocationServiceClient(conn), timeout: CallTimeout}, nil
}

// Close releases the connection during graceful shutdown.
func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) RecordCabberLocation(ctx context.Context, cabberID string, latitude, longitude float64) (time.Time, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	response, err := c.api.RecordCabberLocation(requestid.Outgoing(callCtx), &locationpb.RecordCabberLocationRequest{
		CabberId: cabberID, Latitude: latitude, Longitude: longitude,
	})
	if err != nil {
		return time.Time{}, translate(err)
	}
	return time.Unix(response.GetReceivedAtUnix(), int64(response.GetReceivedAtNanos())).UTC(), nil
}

// translate maps a gRPC status onto the domain results of the port. Nothing of the status message
// travels on: it can carry a value from the request.
func translate(err error) error {
	st := status.Convert(err)
	switch st.Code() {
	case codes.InvalidArgument:
		return fieldOf(st)
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return ErrUnavailable
	default:
		return ErrInternal
	}
}

// The request fields the contract allows in ErrorField (contracts/location.proto). They are the only
// values a field of a peer can take on its way into an answer of ours.
const (
	FieldLatitude  = "latitude"
	FieldLongitude = "longitude"
)

var fields = map[string]bool{FieldLatitude: true, FieldLongitude: true}

// fieldOf reads the first ErrorField detail of a status. A rejection without the detail keeps the
// field empty, and the client then receives a rejection that names no field.
func fieldOf(st *status.Status) error {
	for _, detail := range st.Details() {
		field, ok := detail.(*locationpb.ErrorField)
		if !ok {
			continue
		}
		if fields[field.GetField()] {
			return Invalid{Field: field.GetField(), Reason: field.GetReason()}
		}
	}
	return Invalid{}
}
