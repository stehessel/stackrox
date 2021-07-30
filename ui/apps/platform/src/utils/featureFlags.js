export const types = {
    SHOW_DISALLOWED_CONNECTIONS: 'SHOW_DISALLOWED_CONNECTIONS',
};

// featureFlags defines UI specific feature flags.
export const UIfeatureFlags = {
    [types.SHOW_DISALLOWED_CONNECTIONS]: false,
};

// knownBackendFlags defines backend feature flags that are checked in the UI.
export const knownBackendFlags = {
    ROX_NETWORK_DETECTION_BASELINE_VIOLATION: 'ROX_NETWORK_DETECTION_BASELINE_VIOLATION',
    ROX_NETWORK_DETECTION_BASELINE_SIMULATION: 'ROX_NETWORK_DETECTION_BASELINE_SIMULATION',
    ROX_NETWORK_DETECTION_BLOCKED_FLOWS: 'ROX_NETWORK_DETECTION_BLOCKED_FLOWS',
    ROX_K8S_AUDIT_LOG_DETECTION: 'ROX_K8S_AUDIT_LOG_DETECTION',
    ROX_SCOPED_ACCESS_CONTROL: 'ROX_SCOPED_ACCESS_CONTROL_V2',
    ROX_INACTIVE_IMAGE_SCANNING_UI: 'ROX_INACTIVE_IMAGE_SCANNING_UI',
    ROX_NS_ANNOTATION_FOR_NOTIFIERS: 'ROX_NS_ANNOTATION_FOR_NOTIFIERS',
};

// isBackendFeatureFlagEnabled returns whether a feature flag retrieved from the backend is enabled.
// The default should never be required unless there's a programming error.
export const isBackendFeatureFlagEnabled = (backendFeatureFlags, envVar, defaultVal) => {
    const featureFlag = backendFeatureFlags.find((flag) => flag.envVar === envVar);
    if (!featureFlag) {
        if (process.env.NODE_ENV === 'development') {
            // eslint-disable-next-line no-console
            console.warn(`EnvVar ${envVar} not found in the backend list, possibly stale?`);
        }
        return defaultVal;
    }
    return featureFlag.enabled;
};
