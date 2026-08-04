package protocol

// InstanceRef is the trusted instance identity carried by responses.
type InstanceRef struct {
	InstanceID  string
	DisplayName string
}

// CredentialRef is the credential identity carried by the identity response.
type CredentialRef struct {
	CredentialID string
	Scopes       []string
}

// IdentityResponse is the typed GET /api/v1/export/identity response
// (contract section 5.4).
type IdentityResponse struct {
	Instance   InstanceRef
	Credential CredentialRef
	ServerTime string
}

// InstanceRegistration is the typed one-time registration/issuance result
// (contract section 5.2). Token is disclosed exactly once here.
type InstanceRegistration struct {
	Instance   RegisteredInstance
	Credential IssuedCredential
}

type RegisteredInstance struct {
	InstanceID  string
	DisplayName string
	Enabled     bool
	CreatedAt   string
	UpdatedAt   string
}

type IssuedCredential struct {
	CredentialID string
	Name         string
	Scopes       []string
	Token        *string
	CreatedAt    string
	ExpiresAt    *string
}

// ConnectionTestResponse is the typed non-mutating connection-test success
// response (contract section 8.2).
type ConnectionTestResponse struct {
	OK               bool
	Instance         InstanceRef
	CredentialScopes []string
	LatencyMs        int64
	TestedAt         string
}

// ExporterState is the exact runtime status enum (contract section 8.3).
type ExporterState string

const (
	StateDisabled  ExporterState = "disabled"
	StateStarting  ExporterState = "starting"
	StateConnected ExporterState = "connected"
	StateRetrying  ExporterState = "retrying"
	StateDegraded  ExporterState = "degraded"
	StateBlocked   ExporterState = "blocked"
)

// StatusError is the sanitized last-error object; it never contains remote
// bodies or secrets.
type StatusError struct {
	Code      string
	Message   string
	Retryable bool
	At        string
}

// StatusResponse is the typed runtime status response (contract section 8.3).
type StatusResponse struct {
	State                ExporterState
	Enabled              bool
	TokenConfigured      bool
	Instance             *InstanceRef
	StreamID             *string
	NextSequence         *int64
	AcknowledgedThrough  *int64
	NextExpectedSequence *int64
	BacklogEvents        int64
	BacklogBytes         int64
	OldestBacklogAt      *string
	LastAttemptAt        *string
	LastSuccessAt        *string
	NextRetryAt          *string
	MetadataRevisions    map[string]int64
	LastError            *StatusError
}

// CreateInstanceRequest is the typed admin create-instance request (contract
// section 5.1).
type CreateInstanceRequest struct {
	DisplayName    string
	CredentialName string
	Scopes         []string
}

// ErrorEnvelope is the typed stable error body (contract section 9).
type ErrorEnvelope struct {
	Error Error
}

// knownScopes is the exact v1 scope set; there is no alias for any scope.
var knownScopes = map[string]struct{}{
	"usage:push":    {},
	"metadata:push": {},
	"identity:test": {},
}

func validateScopes(scopes []string) *Error {
	if len(scopes) < 1 || len(scopes) > 3 {
		return protocolError("invalid_field")
	}
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		if _, ok := knownScopes[scope]; !ok {
			return protocolError("invalid_field")
		}
		if _, dup := seen[scope]; dup {
			return protocolError("invalid_field")
		}
		seen[scope] = struct{}{}
	}
	return nil
}

// DecodeIdentityResponse strictly decodes the identity response.
func DecodeIdentityResponse(data []byte) (*IdentityResponse, *Error) {
	if perr := responsePrecheck(data); perr != nil {
		return nil, perr
	}
	var wire struct {
		ProtocolVersion string `json:"protocolVersion"`
		Instance        struct {
			InstanceID  string `json:"instanceId"`
			DisplayName string `json:"displayName"`
		} `json:"instance"`
		Credential struct {
			CredentialID string   `json:"credentialId"`
			Scopes       []string `json:"scopes"`
		} `json:"credential"`
		ServerTime string `json:"serverTime"`
	}
	if perr := decodeTyped(data, &wire); perr != nil {
		return nil, perr
	}
	if !isUUIDv7(wire.Instance.InstanceID) || !stringLenInRange(wire.Instance.DisplayName, 1, 128) ||
		!isUUIDv7(wire.Credential.CredentialID) || !isTimestamp(wire.ServerTime) {
		return nil, protocolError("invalid_field")
	}
	if perr := validateScopes(wire.Credential.Scopes); perr != nil {
		return nil, perr
	}
	return &IdentityResponse{
		Instance:   InstanceRef{InstanceID: wire.Instance.InstanceID, DisplayName: wire.Instance.DisplayName},
		Credential: CredentialRef{CredentialID: wire.Credential.CredentialID, Scopes: wire.Credential.Scopes},
		ServerTime: wire.ServerTime,
	}, nil
}

// DecodeInstanceRegistration strictly decodes the one-time issuance result.
func DecodeInstanceRegistration(data []byte) (*InstanceRegistration, *Error) {
	if perr := responsePrecheck(data); perr != nil {
		return nil, perr
	}
	var wire struct {
		ProtocolVersion string `json:"protocolVersion"`
		Instance        struct {
			InstanceID  string `json:"instanceId"`
			DisplayName string `json:"displayName"`
			Enabled     bool   `json:"enabled"`
			CreatedAt   string `json:"createdAt"`
			UpdatedAt   string `json:"updatedAt"`
		} `json:"instance"`
		Credential struct {
			CredentialID string   `json:"credentialId"`
			Name         string   `json:"name"`
			Scopes       []string `json:"scopes"`
			Token        *string  `json:"token"`
			CreatedAt    string   `json:"createdAt"`
			ExpiresAt    *string  `json:"expiresAt"`
		} `json:"credential"`
	}
	if perr := decodeTyped(data, &wire); perr != nil {
		return nil, perr
	}
	if !isUUIDv7(wire.Instance.InstanceID) || !stringLenInRange(wire.Instance.DisplayName, 1, 128) ||
		!isTimestamp(wire.Instance.CreatedAt) || !isTimestamp(wire.Instance.UpdatedAt) ||
		!isUUIDv7(wire.Credential.CredentialID) || !stringLenInRange(wire.Credential.Name, 1, 128) ||
		!isTimestamp(wire.Credential.CreatedAt) {
		return nil, protocolError("invalid_field")
	}
	if wire.Credential.ExpiresAt != nil && !isTimestamp(*wire.Credential.ExpiresAt) {
		return nil, protocolError("invalid_field")
	}
	if perr := validateScopes(wire.Credential.Scopes); perr != nil {
		return nil, perr
	}
	return &InstanceRegistration{
		Instance: RegisteredInstance{
			InstanceID:  wire.Instance.InstanceID,
			DisplayName: wire.Instance.DisplayName,
			Enabled:     wire.Instance.Enabled,
			CreatedAt:   wire.Instance.CreatedAt,
			UpdatedAt:   wire.Instance.UpdatedAt,
		},
		Credential: IssuedCredential{
			CredentialID: wire.Credential.CredentialID,
			Name:         wire.Credential.Name,
			Scopes:       wire.Credential.Scopes,
			Token:        wire.Credential.Token,
			CreatedAt:    wire.Credential.CreatedAt,
			ExpiresAt:    wire.Credential.ExpiresAt,
		},
	}, nil
}

// DecodeConnectionTestResponse strictly decodes the connection-test success
// response.
func DecodeConnectionTestResponse(data []byte) (*ConnectionTestResponse, *Error) {
	if perr := responsePrecheck(data); perr != nil {
		return nil, perr
	}
	var wire struct {
		ProtocolVersion string `json:"protocolVersion"`
		OK              bool   `json:"ok"`
		Instance        struct {
			InstanceID  string `json:"instanceId"`
			DisplayName string `json:"displayName"`
		} `json:"instance"`
		CredentialScopes []string `json:"credentialScopes"`
		LatencyMs        int64    `json:"latencyMs"`
		TestedAt         string   `json:"testedAt"`
	}
	if perr := decodeTyped(data, &wire); perr != nil {
		return nil, perr
	}
	if !isUUIDv7(wire.Instance.InstanceID) || !stringLenInRange(wire.Instance.DisplayName, 1, 128) ||
		wire.LatencyMs < 0 || wire.LatencyMs > MaxSafeInteger || !isTimestamp(wire.TestedAt) {
		return nil, protocolError("invalid_field")
	}
	if perr := validateScopes(wire.CredentialScopes); perr != nil {
		return nil, perr
	}
	return &ConnectionTestResponse{
		OK:               wire.OK,
		Instance:         InstanceRef{InstanceID: wire.Instance.InstanceID, DisplayName: wire.Instance.DisplayName},
		CredentialScopes: wire.CredentialScopes,
		LatencyMs:        wire.LatencyMs,
		TestedAt:         wire.TestedAt,
	}, nil
}

// DecodeStatusResponse strictly decodes the runtime status response. Token
// material is an unknown field and is rejected.
func DecodeStatusResponse(data []byte) (*StatusResponse, *Error) {
	if perr := responsePrecheck(data); perr != nil {
		return nil, perr
	}
	var wire struct {
		ProtocolVersion string `json:"protocolVersion"`
		State           string `json:"state"`
		Enabled         bool   `json:"enabled"`
		TokenConfigured bool   `json:"tokenConfigured"`
		Instance        *struct {
			InstanceID  string `json:"instanceId"`
			DisplayName string `json:"displayName"`
		} `json:"instance"`
		StreamID             *string          `json:"streamId"`
		NextSequence         *int64           `json:"nextSequence"`
		AcknowledgedThrough  *int64           `json:"acknowledgedThrough"`
		NextExpectedSequence *int64           `json:"nextExpectedSequence"`
		BacklogEvents        int64            `json:"backlogEvents"`
		BacklogBytes         int64            `json:"backlogBytes"`
		OldestBacklogAt      *string          `json:"oldestBacklogAt"`
		LastAttemptAt        *string          `json:"lastAttemptAt"`
		LastSuccessAt        *string          `json:"lastSuccessAt"`
		NextRetryAt          *string          `json:"nextRetryAt"`
		MetadataRevisions    map[string]int64 `json:"metadataRevisions"`
		LastError            *struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			Retryable bool   `json:"retryable"`
			At        string `json:"at"`
		} `json:"lastError"`
	}
	if perr := decodeTyped(data, &wire); perr != nil {
		return nil, perr
	}
	invalid := func() *Error { return protocolError("invalid_field") }
	switch ExporterState(wire.State) {
	case StateDisabled, StateStarting, StateConnected, StateRetrying, StateDegraded, StateBlocked:
	default:
		return nil, invalid()
	}
	if wire.BacklogEvents < 0 || wire.BacklogEvents > MaxSafeInteger ||
		wire.BacklogBytes < 0 || wire.BacklogBytes > MaxSafeInteger {
		return nil, invalid()
	}
	for _, v := range []*int64{wire.NextSequence, wire.AcknowledgedThrough, wire.NextExpectedSequence} {
		if v != nil && (*v < 0 || *v > MaxSafeInteger) {
			return nil, invalid()
		}
	}
	if wire.StreamID != nil && !isUUIDv7(*wire.StreamID) {
		return nil, invalid()
	}
	for _, ts := range []*string{wire.OldestBacklogAt, wire.LastAttemptAt, wire.LastSuccessAt, wire.NextRetryAt} {
		if ts != nil && !isTimestamp(*ts) {
			return nil, invalid()
		}
	}
	for _, category := range []string{"auth_files", "api_keys", "provider_identities"} {
		if _, ok := wire.MetadataRevisions[category]; !ok {
			return nil, invalid()
		}
	}
	status := &StatusResponse{
		State:                ExporterState(wire.State),
		Enabled:              wire.Enabled,
		TokenConfigured:      wire.TokenConfigured,
		StreamID:             wire.StreamID,
		NextSequence:         wire.NextSequence,
		AcknowledgedThrough:  wire.AcknowledgedThrough,
		NextExpectedSequence: wire.NextExpectedSequence,
		BacklogEvents:        wire.BacklogEvents,
		BacklogBytes:         wire.BacklogBytes,
		OldestBacklogAt:      wire.OldestBacklogAt,
		LastAttemptAt:        wire.LastAttemptAt,
		LastSuccessAt:        wire.LastSuccessAt,
		NextRetryAt:          wire.NextRetryAt,
		MetadataRevisions:    wire.MetadataRevisions,
	}
	if wire.Instance != nil {
		if !isUUIDv7(wire.Instance.InstanceID) || !stringLenInRange(wire.Instance.DisplayName, 1, 128) {
			return nil, invalid()
		}
		status.Instance = &InstanceRef{InstanceID: wire.Instance.InstanceID, DisplayName: wire.Instance.DisplayName}
	}
	if wire.LastError != nil {
		if !stringLenInRange(wire.LastError.Code, 1, 64) ||
			!stringLenInRange(wire.LastError.Message, 1, 256) ||
			!isTimestamp(wire.LastError.At) {
			return nil, invalid()
		}
		status.LastError = &StatusError{
			Code:      wire.LastError.Code,
			Message:   wire.LastError.Message,
			Retryable: wire.LastError.Retryable,
			At:        wire.LastError.At,
		}
	}
	return status, nil
}

// DecodeCreateInstanceRequest strictly decodes the admin create-instance
// request and enforces the exact v1 scope set (no aliases).
func DecodeCreateInstanceRequest(data []byte) (*CreateInstanceRequest, *Error) {
	if _, perr := scanStrict(data, true); perr != nil {
		return nil, perr
	}
	var wire struct {
		DisplayName string `json:"displayName"`
		Credential  struct {
			Name   string   `json:"name"`
			Scopes []string `json:"scopes"`
		} `json:"credential"`
	}
	if perr := decodeTyped(data, &wire); perr != nil {
		return nil, perr
	}
	if !stringLenInRange(wire.DisplayName, 1, 128) || !stringLenInRange(wire.Credential.Name, 1, 128) {
		return nil, protocolError("invalid_field")
	}
	if perr := validateScopes(wire.Credential.Scopes); perr != nil {
		return nil, perr
	}
	return &CreateInstanceRequest{
		DisplayName:    wire.DisplayName,
		CredentialName: wire.Credential.Name,
		Scopes:         wire.Credential.Scopes,
	}, nil
}

// DecodeErrorEnvelope strictly decodes the stable error body and requires a
// known section-9 code.
func DecodeErrorEnvelope(data []byte) (*ErrorEnvelope, *Error) {
	if perr := responsePrecheck(data); perr != nil {
		return nil, perr
	}
	var wire struct {
		ProtocolVersion string `json:"protocolVersion"`
		Error           struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	if perr := decodeTyped(data, &wire); perr != nil {
		return nil, perr
	}
	spec, ok := errorTable[wire.Error.Code]
	if !ok {
		return nil, protocolError("invalid_field")
	}
	return &ErrorEnvelope{Error: Error{
		HTTPStatus: spec.status,
		Code:       wire.Error.Code,
		Message:    wire.Error.Message,
		Retryable:  wire.Error.Retryable,
	}}, nil
}
