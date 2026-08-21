import importlib.util
import json
import pathlib
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


MODULE_PATH = pathlib.Path(__file__).with_name("api_client.py")
SPEC = importlib.util.spec_from_file_location("api_client", MODULE_PATH)
CLIENT = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(CLIENT)


class APIHandler(BaseHTTPRequestHandler):
    requests = []

    def do_GET(self):
        if self.path == "/":
            body = b'<meta content="test-token" name="md-viewer-review-token">'
            self.send_response(200)
            self.end_headers()
            self.wfile.write(body)
            return
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(b'{"schemaVersion":1,"documents":[]}')

    def do_PATCH(self):
        length = int(self.headers.get("Content-Length", "0"))
        self.__class__.requests.append((self.path, dict(self.headers), json.loads(self.rfile.read(length))))
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(b'{"annotation":{"status":"acknowledged"},"revision":"next"}')

    def log_message(self, _format, *_args):
        return


class APIClientTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = ThreadingHTTPServer(("127.0.0.1", 0), APIHandler)
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()
        cls.origin = f"http://127.0.0.1:{cls.server.server_port}"

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join()

    def test_viewer_origin(self):
        cases = [
            ("loopback", self.origin + "/", self.origin, None),
            ("localhost", "http://localhost:8080", "http://localhost:8080", None),
            ("remote", "https://example.com", None, "loopback HTTP"),
            ("credentials", "http://user@127.0.0.1:8080", None, "loopback HTTP"),
        ]
        for name, value, expected, error in cases:
            with self.subTest(name=name):
                if error:
                    with self.assertRaisesRegex(ValueError, error):
                        CLIENT.viewer_origin(value)
                else:
                    self.assertEqual(CLIENT.viewer_origin(value), expected)

    def test_queue_and_authenticated_mutation(self):
        queue = CLIENT.request_json(self.origin, "/api/annotations?status=open")
        self.assertEqual(queue["documents"], [])
        token = CLIENT.review_token(self.origin)
        self.assertEqual(token, "test-token")

        result = CLIENT.request_json(
            self.origin,
            "/api/annotations/ann_test",
            method="PATCH",
            body={"document": "README.md", "status": "acknowledged"},
            revision="current",
            token=token,
        )
        self.assertEqual(result["revision"], "next")
        _, headers, body = APIHandler.requests[-1]
        self.assertEqual(headers["Origin"], self.origin)
        self.assertEqual(headers["If-Match"], '"current"')
        self.assertEqual(headers["X-Md-Viewer-Token"], "test-token")
        self.assertEqual(body["status"], "acknowledged")


if __name__ == "__main__":
    unittest.main()
