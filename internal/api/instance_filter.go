package api

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

const allInstancesFilter = "all"

var canonicalInstanceIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

const instanceFilterContextKey = "instance_filter_selection"

type instanceFilterSelection struct {
	InstanceID   string
	InstanceName string
	All          bool
}

func instanceFilterMiddleware(provider CPAInstanceProvider) gin.HandlerFunc {
	return func(c *gin.Context) {
		values, present := c.Request.URL.Query()["instance_id"]
		if !present {
			selection := instanceFilterSelection{InstanceID: entities.LegacyCPAInstanceID, InstanceName: entities.LegacyCPAInstanceName}
			c.Set(instanceFilterContextKey, selection)
			c.Request = c.Request.WithContext(service.ContextWithInstanceFilter(c.Request.Context(), entities.LegacyCPAInstanceID))
			c.Next()
			return
		}
		if len(values) != 1 {
			writeInstanceFilterError(c, http.StatusBadRequest, "invalid_instance_filter")
			c.Abort()
			return
		}
		value := strings.TrimSpace(values[0])
		if value == "" {
			writeInstanceFilterError(c, http.StatusBadRequest, "invalid_instance_filter")
			c.Abort()
			return
		}
		if value == allInstancesFilter {
			selection := instanceFilterSelection{All: true}
			c.Set(instanceFilterContextKey, selection)
			c.Request = c.Request.WithContext(service.ContextWithInstanceFilter(c.Request.Context(), ""))
			c.Next()
			return
		}
		if !canonicalInstanceIDPattern.MatchString(value) {
			writeInstanceFilterError(c, http.StatusBadRequest, "invalid_instance_filter")
			c.Abort()
			return
		}
		if provider == nil {
			selection := instanceFilterSelection{InstanceID: value}
			c.Set(instanceFilterContextKey, selection)
			c.Request = c.Request.WithContext(service.ContextWithInstanceFilter(c.Request.Context(), value))
			c.Next()
			return
		}
		instance, err := provider.Get(c.Request.Context(), value)
		switch {
		case errors.Is(err, service.ErrInstanceNotFound):
			writeInstanceFilterError(c, http.StatusNotFound, "instance_not_found")
			c.Abort()
			return
		case err != nil:
			writeInternalError(c, "validate instance filter failed", err)
			c.Abort()
			return
		case !instance.Enabled:
			writeInstanceFilterError(c, http.StatusConflict, "instance_disabled")
			c.Abort()
			return
		}
		selection := instanceFilterSelection{InstanceID: instance.ID, InstanceName: instance.DisplayName}
		c.Set(instanceFilterContextKey, selection)
		c.Request = c.Request.WithContext(service.ContextWithInstanceFilter(c.Request.Context(), instance.ID))
		c.Next()
	}
}

func instanceFilterFromGin(c *gin.Context) instanceFilterSelection {
	if c == nil {
		return instanceFilterSelection{InstanceID: entities.LegacyCPAInstanceID, InstanceName: entities.LegacyCPAInstanceName}
	}
	selection, ok := c.Get(instanceFilterContextKey)
	if !ok {
		instanceID, present := service.InstanceFilterSelectionFromContext(c.Request.Context())
		if !present {
			return instanceFilterSelection{InstanceID: entities.LegacyCPAInstanceID, InstanceName: entities.LegacyCPAInstanceName}
		}
		return instanceFilterSelection{InstanceID: instanceID, All: instanceID == ""}
	}
	result, ok := selection.(instanceFilterSelection)
	if !ok {
		return instanceFilterSelection{InstanceID: entities.LegacyCPAInstanceID, InstanceName: entities.LegacyCPAInstanceName}
	}
	return result
}

func writeInstanceFilterError(c *gin.Context, status int, code string) {
	c.JSON(status, gin.H{"error": code})
}

func contextWithRequestInstanceFilter(ctx context.Context, c *gin.Context) context.Context {
	if c == nil {
		return ctx
	}
	return service.ContextWithInstanceFilter(ctx, instanceFilterFromGin(c).InstanceID)
}
