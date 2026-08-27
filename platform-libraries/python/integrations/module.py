"""
velane.integrations — built-in integration proxy client

Lets snippets call any OAuth-connected provider API without handling
credentials. Auth is managed by the platform; tokens are never exposed
to snippet code.

Usage:
    from velane.integrations import integration

    github = integration("github")
    user   = github.get("/user")
    issue  = github.post("/repos/owner/repo/issues", {"title": "Bug"})
    github.patch("/repos/owner/repo/issues/1", {"state": "closed"})
    github.delete("/repos/owner/repo/labels/old-label")

The provider slug must match a connected integration in your Velane dashboard.
Paths are the provider's native API paths — see the Integrations tab for docs.
"""

import json
import os
from urllib.error import HTTPError
from urllib.request import Request, urlopen

_PROXY_URL = os.environ.get("VELANE_PROXY_URL", "")
_TENANT_ID = os.environ.get("VELANE_TENANT_ID", "")
# Signed by the control plane; carries the invoking user's group access so the
# proxy can enforce integration grants. Opaque and unforgeable from here.
_CALLER_TOKEN = os.environ.get("VELANE_CALLER_TOKEN", "")


class IntegrationClient:
    def __init__(self, provider: str, alias: str | None = None) -> None:
        self._provider = provider
        self._alias = alias

    def _req(self, method: str, endpoint: str, body=None):
        if not _PROXY_URL:
            raise RuntimeError("velane.integrations: VELANE_PROXY_URL is not set")
        if not _TENANT_ID:
            raise RuntimeError("velane.integrations: VELANE_TENANT_ID is not set")

        url = f"{_PROXY_URL}/v1/proxy/{self._provider}{endpoint}"
        data = json.dumps(body).encode() if body is not None else None
        req = Request(
            url,
            data=data,
            method=method,
            headers={
                "Content-Type": "application/json",
                "X-Velane-Tenant": _TENANT_ID,
                **({"X-Velane-Caller": _CALLER_TOKEN} if _CALLER_TOKEN else {}),
                **({"X-Velane-Integration-Alias": self._alias} if self._alias else {}),
            },
        )
        try:
            with urlopen(req) as resp:
                raw = resp.read()
                ct = resp.headers.get("Content-Type", "")
                if not raw:
                    return None
                if "application/json" in ct:
                    return json.loads(raw)
                return raw.decode()
        except HTTPError as exc:
            body_text = exc.read().decode(errors="replace")
            raise RuntimeError(
                f"[{self._provider}] {method} {endpoint} → HTTP {exc.code}: {body_text}"
            ) from exc

    def get(self, endpoint: str):
        """GET request to the provider API."""
        return self._req("GET", endpoint)

    def post(self, endpoint: str, body=None):
        """POST request to the provider API."""
        return self._req("POST", endpoint, body)

    def patch(self, endpoint: str, body=None):
        """PATCH request to the provider API."""
        return self._req("PATCH", endpoint, body)

    def put(self, endpoint: str, body=None):
        """PUT request to the provider API."""
        return self._req("PUT", endpoint, body)

    def delete(self, endpoint: str):
        """DELETE request to the provider API."""
        return self._req("DELETE", endpoint)


def integration(provider: str, opts: dict | None = None) -> IntegrationClient:
    """Returns a client for the given connected integration provider.

    Args:
        provider: Provider slug, e.g. "github", "salesforce", "slack"
    """
    alias = None
    if isinstance(opts, dict):
        alias = opts.get("alias")
    return IntegrationClient(provider, alias=alias)
