import io
import json
import os
import unittest
from unittest.mock import patch
from urllib.error import HTTPError

from store.module import Store


class FakeResponse:
    def __init__(self, payload=b"{}"):
        self.payload = payload
        self.closed = False

    def read(self):
        return self.payload

    def close(self):
        self.closed = True

    def __enter__(self):
        return self

    def __exit__(self, *_):
        self.close()


class StoreTests(unittest.TestCase):
    def setUp(self):
        self.env = patch.dict(
            os.environ,
            {
                "VELANE_PROXY_URL": "http://control-plane:8080",
                "VELANE_TENANT_ID": "tenant-123",
            },
        )
        self.env.start()
        self.addCleanup(self.env.stop)

    @patch("store.module.urlopen")
    def test_encodes_key_routes_namespace_sends_header_and_passes_ttl(self, urlopen):
        urlopen.return_value = FakeResponse()

        Store().namespace("sync/jobs").set("cursor/a.b", {"page": 2}, ttl=60)

        request = urlopen.call_args.args[0]
        self.assertEqual(
            request.full_url,
            "http://control-plane:8080/v1/internal/kv/entry?namespace=sync%2Fjobs&key=cursor%2Fa.b",
        )
        self.assertEqual(request.get_method(), "PUT")
        self.assertEqual(request.get_header("X-velane-tenant"), "tenant-123")
        self.assertEqual(request.get_header("Content-type"), "application/json")
        self.assertEqual(json.loads(request.data), {"value": {"page": 2}, "ttl_seconds": 60})
        self.assertEqual(urlopen.call_args.kwargs["timeout"], 10)

    @patch("store.module.urlopen")
    def test_get_returns_json_value(self, urlopen):
        urlopen.return_value = FakeResponse(b'{"value": [true, null, 3]}')

        self.assertEqual(Store().get("result"), [True, None, 3])

    @patch("store.module.urlopen")
    def test_get_returns_none_for_missing_key(self, urlopen):
        urlopen.side_effect = HTTPError("http://example", 404, "Not Found", {}, io.BytesIO(b"missing"))

        self.assertIsNone(Store().get("missing"))

    @patch("store.module.urlopen")
    def test_delete_returns_false_for_missing_key(self, urlopen):
        urlopen.side_effect = HTTPError("http://example", 404, "Not Found", {}, io.BytesIO(b"missing"))

        self.assertFalse(Store().delete("missing"))

    def test_missing_proxy_or_tenant_is_rejected(self):
        with patch.dict(os.environ, {}, clear=True):
            with self.assertRaisesRegex(RuntimeError, "VELANE_PROXY_URL is not set"):
                Store().get("key")

        with patch.dict(os.environ, {"VELANE_PROXY_URL": "http://control-plane:8080"}, clear=True):
            with self.assertRaisesRegex(RuntimeError, "VELANE_TENANT_ID is not set"):
                Store().get("key")

    @patch("store.module.urlopen")
    def test_non_404_errors_include_method_key_and_status(self, urlopen):
        urlopen.side_effect = HTTPError("http://example", 403, "Forbidden", {}, io.BytesIO(b"denied"))

        with self.assertRaisesRegex(RuntimeError, r"\[store\] PUT cursor → HTTP 403: denied"):
            Store().set("cursor", 1)


if __name__ == "__main__":
    unittest.main()
