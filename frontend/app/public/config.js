// Runtime configuration. The container entrypoint (Task 20) renders this file
// from environment variables so one image serves any environment.
// Empty oidcAuthority/oidcClientId => the UI shows "SSO not configured".
window.__AGENTGATE_CONFIG__ = {
  apiUrl: "",
  oidcAuthority: "",
  oidcClientId: "",
  oidcScope: "openid profile",
};
