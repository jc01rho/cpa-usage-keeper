package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/protocol"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

const adminInstanceBodyLimit int64 = protocol.MaxBodyBytes

type CPAInstanceProvider interface {
	Create(context.Context, service.CreateInstanceInput) (service.IssuedInstanceCredential, error)
	List(context.Context) ([]entities.CPAInstance, error)
	Get(context.Context, string) (entities.CPAInstance, error)
	Update(context.Context, string, *string, *bool) (entities.CPAInstance, error)
	Issue(context.Context, string, service.CredentialInput) (service.IssuedInstanceCredential, error)
	ListCredentials(context.Context, string) ([]entities.CPAInstanceCredential, error)
	Rotate(context.Context, string, string, service.CredentialInput) (service.IssuedInstanceCredential, error)
	Revoke(context.Context, string, string) error
	Authenticate(context.Context, string) (service.AuthenticatedIngestCredential, error)
}

type CPAInstanceDeleter interface {
	Delete(context.Context, string) error
}

type createInstanceWire struct {
	DisplayName string `json:"displayName"`
	Credential  struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	} `json:"credential"`
}

type credentialWire struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresAt *string  `json:"expiresAt"`
}

type patchInstanceWire struct {
	DisplayName *string `json:"displayName"`
	Enabled     *bool   `json:"enabled"`
}

type instanceObject struct {
	InstanceID  string `json:"instanceId"`
	DisplayName string `json:"displayName"`
	Enabled     bool   `json:"enabled"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type credentialSummary struct {
	CredentialID string   `json:"credentialId"`
	Name         string   `json:"name"`
	Scopes       []string `json:"scopes"`
	Active       bool     `json:"active"`
	CreatedAt    string   `json:"createdAt"`
	ExpiresAt    *string  `json:"expiresAt"`
	LastUsedAt   *string  `json:"lastUsedAt"`
	RevokedAt    *string  `json:"revokedAt"`
}

type issuedCredentialObject struct {
	CredentialID string   `json:"credentialId"`
	Name         string   `json:"name"`
	Scopes       []string `json:"scopes"`
	Token        string   `json:"token"`
	CreatedAt    string   `json:"createdAt"`
	ExpiresAt    *string  `json:"expiresAt"`
}

func registerInstanceRoutes(router *gin.RouterGroup, provider CPAInstanceProvider) {
	if provider == nil {
		return
	}
	router.POST("/instances", func(c *gin.Context) {
		body, ok := readAdminJSON(c)
		if !ok {
			return
		}
		decoded, perr := protocol.DecodeCreateInstanceRequest(body)
		if perr != nil {
			writeProtocolError(c, perr.Code)
			return
		}
		if !validAdminName(decoded.DisplayName) || !validAdminName(decoded.CredentialName) {
			writeProtocolError(c, "invalid_field")
			return
		}
		if !service.HasScope(decoded.Scopes, service.ScopeIdentityTest) {
			writeProtocolError(c, "invalid_field")
			return
		}
		result, err := provider.Create(c.Request.Context(), service.CreateInstanceInput{DisplayName: decoded.DisplayName, CredentialName: decoded.CredentialName, Scopes: decoded.Scopes})
		if err != nil {
			writeLifecycleError(c, "create instance", err)
			return
		}
		logrus.WithFields(logrus.Fields{"instance_id": result.Instance.ID, "credential_id": result.Credential.ID}).Info("CPA instance created")
		c.JSON(http.StatusCreated, gin.H{"protocolVersion": protocol.ProtocolVersion, "instance": instanceResponse(result.Instance), "credential": issuedCredentialResponse(result.Credential, result.Token)})
	})
	router.GET("/instances", func(c *gin.Context) {
		rows, err := provider.List(c.Request.Context())
		if err != nil {
			writeLifecycleError(c, "list instances", err)
			return
		}
		items := make([]instanceObject, 0, len(rows))
		for _, row := range rows {
			items = append(items, instanceResponse(row))
		}
		c.JSON(http.StatusOK, gin.H{"protocolVersion": protocol.ProtocolVersion, "instances": items})
	})
	router.GET("/instances/:instanceId", func(c *gin.Context) {
		row, err := provider.Get(c.Request.Context(), c.Param("instanceId"))
		if err != nil {
			writeLifecycleError(c, "get instance", err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"protocolVersion": protocol.ProtocolVersion, "instance": instanceResponse(row)})
	})
	router.PATCH("/instances/:instanceId", func(c *gin.Context) {
		var request patchInstanceWire
		if !decodeAdminJSON(c, &request) {
			return
		}
		if request.DisplayName == nil && request.Enabled == nil {
			writeProtocolError(c, "invalid_field")
			return
		}
		if request.DisplayName != nil && !validAdminName(*request.DisplayName) {
			writeProtocolError(c, "invalid_field")
			return
		}
		row, err := provider.Update(c.Request.Context(), c.Param("instanceId"), request.DisplayName, request.Enabled)
		if err != nil {
			writeLifecycleError(c, "update instance", err)
			return
		}
		logrus.WithField("instance_id", row.ID).Info("CPA instance updated")
		c.JSON(http.StatusOK, gin.H{"protocolVersion": protocol.ProtocolVersion, "instance": instanceResponse(row)})
	})
	if deleter, ok := provider.(CPAInstanceDeleter); ok {
		router.DELETE("/instances/:instanceId", func(c *gin.Context) {
			if err := deleter.Delete(c.Request.Context(), c.Param("instanceId")); err != nil {
				writeLifecycleError(c, "delete instance", err)
				return
			}
			logrus.WithField("instance_id", c.Param("instanceId")).Warn("CPA instance permanently deleted")
			c.Status(http.StatusNoContent)
		})
	}
	router.POST("/instances/:instanceId/credentials", func(c *gin.Context) {
		input, ok := decodeCredentialInput(c)
		if !ok {
			return
		}
		result, err := provider.Issue(c.Request.Context(), c.Param("instanceId"), input)
		if err != nil {
			writeLifecycleError(c, "issue credential", err)
			return
		}
		logrus.WithFields(logrus.Fields{"instance_id": result.Instance.ID, "credential_id": result.Credential.ID}).Info("CPA instance credential issued")
		c.JSON(http.StatusCreated, gin.H{"protocolVersion": protocol.ProtocolVersion, "credential": issuedCredentialResponse(result.Credential, result.Token)})
	})
	router.GET("/instances/:instanceId/credentials", func(c *gin.Context) {
		rows, err := provider.ListCredentials(c.Request.Context(), c.Param("instanceId"))
		if err != nil {
			writeLifecycleError(c, "list credentials", err)
			return
		}
		items := make([]credentialSummary, 0, len(rows))
		now := time.Now().UTC()
		for _, row := range rows {
			items = append(items, credentialSummaryResponse(row, now))
		}
		c.JSON(http.StatusOK, gin.H{"protocolVersion": protocol.ProtocolVersion, "credentials": items})
	})
	router.POST("/instances/:instanceId/credentials/:credentialId/rotate", func(c *gin.Context) {
		input, ok := decodeCredentialInput(c)
		if !ok {
			return
		}
		result, err := provider.Rotate(c.Request.Context(), c.Param("instanceId"), c.Param("credentialId"), input)
		if err != nil {
			writeLifecycleError(c, "rotate credential", err)
			return
		}
		logrus.WithFields(logrus.Fields{"instance_id": result.Instance.ID, "credential_id": c.Param("credentialId"), "replacement_credential_id": result.Credential.ID}).Info("CPA instance credential rotated")
		c.JSON(http.StatusCreated, gin.H{"protocolVersion": protocol.ProtocolVersion, "credential": issuedCredentialResponse(result.Credential, result.Token)})
	})
	router.DELETE("/instances/:instanceId/credentials/:credentialId", func(c *gin.Context) {
		if err := provider.Revoke(c.Request.Context(), c.Param("instanceId"), c.Param("credentialId")); err != nil {
			writeLifecycleError(c, "revoke credential", err)
			return
		}
		logrus.WithFields(logrus.Fields{"instance_id": c.Param("instanceId"), "credential_id": c.Param("credentialId")}).Info("CPA instance credential revoked")
		c.Status(http.StatusNoContent)
	})
}

func decodeCredentialInput(c *gin.Context) (service.CredentialInput, bool) {
	var request credentialWire
	if !decodeAdminJSON(c, &request) {
		return service.CredentialInput{}, false
	}
	if !validAdminName(request.Name) || validateAdminScopes(request.Scopes) != nil {
		writeProtocolError(c, "invalid_field")
		return service.CredentialInput{}, false
	}
	var expires *time.Time
	if request.ExpiresAt != nil {
		parsed, err := time.Parse("2006-01-02T15:04:05.000Z07:00", *request.ExpiresAt)
		if err != nil || parsed.Format("2006-01-02T15:04:05.000Z07:00") != *request.ExpiresAt {
			writeProtocolError(c, "invalid_field")
			return service.CredentialInput{}, false
		}
		expires = &parsed
	}
	return service.CredentialInput{Name: request.Name, Scopes: request.Scopes, ExpiresAt: expires}, true
}

func readAdminJSON(c *gin.Context) ([]byte, bool) {
	if !validJSONHeaders(c) {
		return nil, false
	}
	if c.Request.ContentLength > adminInstanceBodyLimit {
		writeProtocolError(c, "request_too_large")
		return nil, false
	}
	reader := http.MaxBytesReader(c.Writer, c.Request.Body, adminInstanceBodyLimit)
	body, err := io.ReadAll(reader)
	if err != nil {
		if isRequestEntityTooLarge(err) {
			writeProtocolError(c, "request_too_large")
		} else {
			writeProtocolError(c, "invalid_json")
		}
		return nil, false
	}
	return body, true
}

func decodeAdminJSON(c *gin.Context, target interface{}) bool {
	body, ok := readAdminJSON(c)
	if !ok {
		return false
	}
	if perr := protocol.StrictDecode(body, target, false); perr != nil {
		writeProtocolError(c, perr.Code)
		return false
	}
	return true
}

func validJSONHeaders(c *gin.Context) bool {
	ct := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type")))
	if ct != "application/json" && ct != "application/json; charset=utf-8" {
		writeProtocolError(c, "invalid_field")
		return false
	}
	if c.GetHeader("Content-Encoding") != "" {
		writeProtocolError(c, "invalid_field")
		return false
	}
	return true
}

func validateAdminScopes(scopes []string) error {
	if len(scopes) < 1 || len(scopes) > 3 {
		return errors.New("invalid")
	}
	seen := map[string]bool{}
	for _, scope := range scopes {
		if seen[scope] || (scope != service.ScopeUsagePush && scope != service.ScopeMetadataPush && scope != service.ScopeIdentityTest) {
			return errors.New("invalid")
		}
		seen[scope] = true
	}
	return nil
}
func validAdminName(value string) bool {
	return len(value) >= 1 && len(value) <= 128 && strings.TrimSpace(value) == value
}
func instanceResponse(row entities.CPAInstance) instanceObject {
	return instanceObject{row.ID, row.DisplayName, row.Enabled, protocolTime(row.CreatedAt), protocolTime(row.UpdatedAt)}
}
func issuedCredentialResponse(row entities.CPAInstanceCredential, token string) issuedCredentialObject {
	return issuedCredentialObject{row.ID, row.Name, service.CanonicalScopes(strings.Fields(row.Scopes)), token, protocolTime(row.CreatedAt), protocolTimePtr(row.ExpiresAt)}
}
func credentialSummaryResponse(row entities.CPAInstanceCredential, now time.Time) credentialSummary {
	return credentialSummary{row.ID, row.Name, service.CanonicalScopes(strings.Fields(row.Scopes)), row.RevokedAt == nil && (row.ExpiresAt == nil || row.ExpiresAt.After(now)), protocolTime(row.CreatedAt), protocolTimePtr(row.ExpiresAt), protocolTimePtr(row.LastUsedAt), protocolTimePtr(row.RevokedAt)}
}
func protocolTime(value time.Time) string { return value.UTC().Format("2006-01-02T15:04:05.000Z") }
func protocolTimePtr(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := protocolTime(*value)
	return &formatted
}

func writeLifecycleError(c *gin.Context, action string, err error) {
	switch {
	case errors.Is(err, service.ErrInstanceNotFound):
		writeProtocolError(c, "instance_not_found")
	case errors.Is(err, service.ErrCredentialNotFound):
		writeProtocolError(c, "credential_not_found")
	case errors.Is(err, service.ErrLegacyInstance), errors.Is(err, service.ErrActiveCredentials):
		writeProtocolError(c, "instance_state_conflict")
	default:
		logrus.WithError(err).Error(action + " failed")
		writeProtocolError(c, "storage_error")
	}
}
