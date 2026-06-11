import os
import unittest
from unittest.mock import Mock, patch

from client import report_scan_results
from sync import auth_headers


class AuthTest(unittest.TestCase):
    def test_auth_headers_prefers_explicit_token(self):
        with patch.dict(os.environ, {"SERVER_API_TOKEN": "env-token"}):
            self.assertEqual(auth_headers("cli-token"), {"Authorization": "Bearer cli-token"})

    def test_auth_headers_reads_env_token(self):
        with patch.dict(os.environ, {"SERVER_API_TOKEN": "env-token"}):
            self.assertEqual(auth_headers(), {"Authorization": "Bearer env-token"})

    def test_auth_headers_ignores_empty_and_placeholder_token(self):
        with patch.dict(os.environ, {"SERVER_API_TOKEN": "change-me"}):
            self.assertEqual(auth_headers(), {})
            self.assertEqual(auth_headers(""), {})

    @patch("client.requests.post")
    def test_report_scan_results_sends_authorization_header(self, post_mock):
        response = Mock()
        response.json.return_value = {"accepted": 0}
        response.raise_for_status.return_value = None
        post_mock.return_value = response

        report_scan_results("http://server", [], "D:/docs", "host-1", token="cli-token")

        _, kwargs = post_mock.call_args
        self.assertEqual(kwargs["headers"], {"Authorization": "Bearer cli-token"})


if __name__ == "__main__":
    unittest.main()
