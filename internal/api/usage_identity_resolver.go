package api

import (
	"strings"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
)

type usageIdentityResolver struct {
	authFilesByIdentity map[string]entities.UsageIdentity
	providersByIdentity map[string]entities.UsageIdentity
}

func newUsageIdentityResolver(identities []entities.UsageIdentity) usageIdentityResolver {
	authFilesByIdentity := make(map[string]entities.UsageIdentity, len(identities))
	providersByIdentity := make(map[string]entities.UsageIdentity, len(identities))
	for _, identity := range identities {
		if identity.IsDeleted {
			continue
		}
		key := usageIdentityResolverKey(identity.InstanceID, identity.Identity)
		if key == "" {
			continue
		}
		switch identity.AuthType {
		case entities.UsageIdentityAuthTypeAuthFile:
			authFilesByIdentity[key] = identity
		case entities.UsageIdentityAuthTypeAIProvider:
			providersByIdentity[key] = identity
		}
	}

	return usageIdentityResolver{
		authFilesByIdentity: authFilesByIdentity,
		providersByIdentity: providersByIdentity,
	}
}

type resolvedUsageIdentity struct {
	DisplayName string
	Type        string
}

func resolvedUsageIdentityFromEntity(item entities.UsageIdentity) resolvedUsageIdentity {
	return resolvedUsageIdentity{
		DisplayName: helper.UsageIdentityDisplayName(item),
		Type:        strings.TrimSpace(item.Type),
	}
}

func (r usageIdentityResolver) resolveByAuthIndex(authIndex string) (resolvedUsageIdentity, bool) {
	return r.resolveByInstanceAndAuthIndex("", authIndex)
}

func (r usageIdentityResolver) resolveByInstanceAndAuthIndex(instanceID, authIndex string) (resolvedUsageIdentity, bool) {
	key := usageIdentityResolverKey(instanceID, authIndex)
	if key == "" {
		return resolvedUsageIdentity{}, false
	}
	if identity, ok := r.providersByIdentity[key]; ok {
		return resolvedUsageIdentityFromEntity(identity), true
	}
	if identity, ok := r.authFilesByIdentity[key]; ok {
		return resolvedUsageIdentityFromEntity(identity), true
	}
	return resolvedUsageIdentity{}, false
}

func usageIdentityResolverKey(instanceID, authIndex string) string {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		instanceID = entities.LegacyCPAInstanceID
	}
	return instanceID + "\x00" + strings.TrimSpace(authIndex)
}
