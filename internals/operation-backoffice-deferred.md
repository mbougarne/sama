# Operation backoffice deferred issues

- The shared `ProblemSummary` conflict text mentions retaining the last workspace
  owner even for operation conflicts. Operation controls add specific context and
  block until evidence is reloaded. A later shared-error-copy change can remove
  the settings-specific wording without changing error handling.
- The existing `/auth/reauthenticate` endpoint initiates OIDC with an HTTP redirect,
  while the JSON transport rejects redirects and has no browser-navigation flow
  for that endpoint. The operation UI stops on recent-authentication failures and
  directs the operator to sign in with fresh authentication. Integrated forced
  reauthentication and real identity-provider acceptance remain future work;
  signing in with an old IdP session may still require fresh authentication at
  the identity provider. No recent-auth gate is bypassed.
