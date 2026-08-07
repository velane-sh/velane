"""Tenant-scoped JSON state that persists between Velane invocations."""

import json
import os
from urllib.error import HTTPError
from urllib.parse import quote
from urllib.request import Request, urlopen


class Store:
    def __init__(self, namespace: str = "default") -> None:
        self._namespace = namespace

    def namespace(self, namespace: str) -> "Store":
        """Return a client isolated to the given namespace."""
        return Store(namespace)

    def get(self, key: str):
        """Return a JSON value, or None when the key does not exist."""
        try:
            response = self._request("GET", key)
        except HTTPError as exc:
            if exc.code == 404:
                return None
            self._raise_http_error(exc, "GET", key)

        with response:
            return json.loads(response.read())["value"]

    def set(self, key: str, value, *, ttl: int | None = None) -> None:
        """Store a JSON value. ttl, when supplied, is measured in seconds."""
        body = {"value": value}
        if ttl is not None:
            body["ttl_seconds"] = ttl
        try:
            response = self._request("PUT", key, body)
        except HTTPError as exc:
            self._raise_http_error(exc, "PUT", key)
        response.close()

    def delete(self, key: str) -> bool:
        """Delete a key and return whether it existed."""
        try:
            response = self._request("DELETE", key)
        except HTTPError as exc:
            if exc.code == 404:
                return False
            self._raise_http_error(exc, "DELETE", key)

        response.close()
        return True

    def _request(self, method: str, key: str, body=None):
        proxy_url = os.environ.get("VELANE_PROXY_URL", "")
        tenant_id = os.environ.get("VELANE_TENANT_ID", "")
        if not proxy_url:
            raise RuntimeError("@velane/store: VELANE_PROXY_URL is not set")
        if not tenant_id:
            raise RuntimeError("@velane/store: VELANE_TENANT_ID is not set")

        url = (
            f"{proxy_url}/v1/internal/kv/entry?namespace={quote(self._namespace, safe='')}"
            f"&key={quote(key, safe='')}"
        )
        data = None
        headers = {"X-Velane-Tenant": tenant_id}
        if body is not None:
            data = json.dumps(body, allow_nan=False).encode()
            headers["Content-Type"] = "application/json"
        request = Request(url, data=data, method=method, headers=headers)
        return urlopen(request, timeout=10)

    @staticmethod
    def _raise_http_error(exc: HTTPError, method: str, key: str) -> None:
        body = exc.read().decode(errors="replace")
        raise RuntimeError(f"[store] {method} {key} → HTTP {exc.code}: {body}") from exc


# Default tenant-wide namespace. Prefer namespace() to isolate workflow state.
store = Store()
