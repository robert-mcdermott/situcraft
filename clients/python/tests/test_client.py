"""The Python client against a running SituCraft (wave 9, slice 7): the simulated tissue opened,
clustered and labeled through the client; its tables as data frames, each column of one type; the
tissue and a map written as files; results equal to the HTTP API's for the same actions; what an
action changed; errors carrying SituCraft's reason; and SituCraft found through its connection
file. Adapted from MaveScape 0.8.0's tests (the same author's).

clients/test-clients.mjs starts SituCraft with remote control and a browser page, then runs these
tests with SITUCRAFT_TEST_URL, SITUCRAFT_TEST_TOKEN, SITUCRAFT_TEST_DIR and SITUCRAFT_DATA_DIR set.
Without them the tests that need SituCraft are skipped.

    PYTHONPATH=clients/python/src python3 -m unittest discover -s clients/python/tests -v
"""

import json
import os
import unittest
import urllib.request
from pathlib import Path

import situcraft

URL = os.environ.get("SITUCRAFT_TEST_URL")
TOKEN = os.environ.get("SITUCRAFT_TEST_TOKEN")
OUT = Path(os.environ.get("SITUCRAFT_TEST_DIR", "."))
PNG = b"\x89PNG\r\n\x1a\n"
JPEG = b"\xff\xd8\xff"


def http(action, args=None):
    """The same action sent to the HTTP API directly, as docs/REMOTE-CONTROL.md shows."""
    body = json.dumps({"action": action, "args": args or {}, "client": "Python"}).encode("utf-8")
    request = urllib.request.Request(URL + "/api/remote/action", data=body, headers={"Content-Type": "application/json", "X-SituCraft-Token": TOKEN})
    with urllib.request.urlopen(request, timeout=600) as response:
        return json.loads(response.read().decode("utf-8"))


@unittest.skipUnless(URL, "needs a running SituCraft (node clients/test-clients.mjs)")
class ClientTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        # No address or token given: found in remote.json in SITUCRAFT_DATA_DIR.
        cls.sc = situcraft.connect(client="Python")
        cls.sc.open_example("simulated")
        cls.project = cls.sc.get_state()["project"]["name"]
        cls.sc.cluster_cells()
        cls.labels = cls.sc.label_cells(markers="simulated")["id"]

    def test_connects_through_the_connection_file(self):
        self.assertEqual(self.sc.url, URL)
        self.assertEqual(self.sc.token, TOKEN)
        self.assertEqual(self.sc.version(), situcraft.__version__, "the client's version is the program's")
        self.assertEqual(self.sc.api, situcraft.API_VERSION, "the client is for the program's version of the actions")

    def test_every_action_is_a_method(self):
        names = [tool["name"] for tool in self.sc.tools()]
        missing = [name for name in names if not callable(getattr(self.sc, name, None))]
        self.assertGreater(len(names), 80)
        self.assertEqual(missing, [])
        self.assertIn("tree of conditions", self.sc.query_cells.__doc__)

    def test_tables(self):
        cells = self.sc.query_cells({"labels": self.labels, "in": ["epithelial-like"]}, project=self.project, by=self.labels, columns=["transcripts"], limit=0)
        rows = cells.records("rows")
        self.assertEqual(len(rows), cells["kept"])
        self.assertEqual(list(rows[0]), ["row", "id", "x", "y", "transcripts"])
        composition = cells.records()  # the first table: the composition, before the page of cells
        self.assertEqual(sum(r["kept"] for r in composition), cells["kept"])
        listed = self.sc.describe_project(project=self.project).records("results")
        self.assertIn(self.labels, [r["id"] for r in listed])
        try:
            import pandas  # noqa: F401
        except ImportError:
            self.skipTest("pandas is not installed")
        frame = cells.frame("rows")
        self.assertEqual(list(frame.columns), ["row", "id", "x", "y", "transcripts"])
        self.assertEqual(len(frame), cells["kept"])
        self.assertEqual(str(frame["transcripts"].dtype), "int64")
        numbers = self.sc.get_result(self.labels, limit=0)
        table = numbers.frame()
        self.assertEqual(list(table.columns), numbers["table"]["columns"])
        self.assertEqual(len(table), numbers["table"]["total"])

    def test_results_equal_the_http_api(self):
        sc = self.sc
        cases = [
            (lambda: sc.describe_project(project=self.project), "describe_project", {"project": self.project}),
            (lambda: sc.query_cells({"attribute": "transcripts", "min": 200}, project=self.project), "query_cells", {"where": {"attribute": "transcripts", "min": 200}, "project": self.project}),
            (lambda: sc.get_result(self.labels, limit=3), "get_result", {"id": self.labels, "limit": 3}),
            (lambda: sc.call("qc_metrics"), "qc_metrics", {}),
        ]
        for method, action, args in cases:
            with self.subTest(action=action):
                mine = method()
                theirs = http(action, args)
                self.assertTrue(theirs["ok"], theirs.get("message"))
                self.assertEqual(mine.message, theirs["message"])
                self.assertEqual(mine.data, theirs["data"])

    def test_pictures_written_as_files(self):
        tissue = OUT / "python-tissue.jpg"
        result = self.sc.show_tissue(file=str(tissue), picture_width=400)
        self.assertEqual(tissue.read_bytes()[:3], JPEG)
        self.assertEqual([i["type"] for i in result.images()], ["jpeg"])
        drawn = OUT / "python-map.png"
        self.sc.show_map(file=str(drawn), picture_width=400)
        self.assertEqual(drawn.read_bytes()[:8], PNG)

    def test_what_an_action_changed(self):
        saved = self.sc.save_filter("Epithelial-like", where={"labels": self.labels, "in": ["epithelial-like"]})
        self.assertEqual([(c["kind"], c["change"]) for c in saved.changed], [("filter", "added")])
        self.assertEqual(saved.changed[0]["id"], saved["id"])

    def test_exports_need_the_token_and_never_replace_a_file(self):
        path = OUT / "python-methods.md"
        path.unlink(missing_ok=True)
        result = self.sc.export("methods", path=str(path), project=True)
        self.assertTrue(path.read_text(encoding="utf-8").startswith("# Methods"), result.message)
        with self.assertRaises(situcraft.SituCraftError) as again:
            self.sc.export("methods", path=str(path), project=True)
        self.assertIn("overwrite", str(again.exception))
        without = situcraft.SituCraft(URL, "")
        with self.assertRaises(situcraft.SituCraftError) as refused:
            without.export("methods", path=str(OUT / "python-other.md"), project=True)
        self.assertEqual(refused.exception.status, 401)

    def test_errors_carry_the_reason(self):
        with self.assertRaises(situcraft.SituCraftError) as unknown:
            self.sc.call("no_such_action")
        self.assertIn('Unknown action "no_such_action"', str(unknown.exception))
        with self.assertRaises(situcraft.SituCraftError) as wrong:
            self.sc.query_cells({"attribute": "size", "min": 1}, project=self.project)
        self.assertIn('no attribute "size"', str(wrong.exception))
        self.assertEqual(wrong.exception.action, "query_cells")
        with self.assertRaises(situcraft.SituCraftError) as gone:
            situcraft.connect("http://127.0.0.1:9", "")
        self.assertEqual(gone.exception.status, 0)


class OfflineTest(unittest.TestCase):
    """What the client does without a SituCraft."""

    def test_the_connection_file_and_the_environment(self):
        import tempfile

        saved = {k: os.environ.pop(k, None) for k in ("SITUCRAFT_URL", "SITUCRAFT_TOKEN", "SITUCRAFT_DATA_DIR")}
        try:
            with tempfile.TemporaryDirectory() as folder:
                self.assertEqual(situcraft.find_connection(folder), {"url": "http://127.0.0.1:8840", "token": None})
                Path(folder, "remote.json").write_text(json.dumps({"url": "http://127.0.0.1:8899", "token": "abc", "version": "x", "pid": 1}))
                self.assertEqual(situcraft.find_connection(folder), {"url": "http://127.0.0.1:8899", "token": "abc"})
                os.environ["SITUCRAFT_URL"] = "http://127.0.0.1:8900"
                self.assertEqual(situcraft.find_connection(folder)["url"], "http://127.0.0.1:8900")
        finally:
            for key, value in saved.items():
                os.environ.pop(key, None)
                if value is not None:
                    os.environ[key] = value

    def test_another_version_of_the_actions_is_said(self):
        import http.server
        import threading
        import warnings

        answers = []

        class Program(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                body = json.dumps(answers[-1]).encode("utf-8")
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(body)

            def log_message(self, *args):
                pass

        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Program)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        url = f"http://127.0.0.1:{server.server_address[1]}"
        try:
            for api, said in ((situcraft.API_VERSION, None), (situcraft.API_VERSION + 1, "update the client"), (situcraft.API_VERSION - 1, "update SituCraft"), (None, None)):
                answers.append({"version": "9.9.9", "tools": [], **({"api": api} if api is not None else {})})
                with warnings.catch_warnings(record=True) as caught:
                    warnings.simplefilter("always")
                    sc = situcraft.connect(url, "")
                self.assertEqual(sc.api, api)
                if said:
                    self.assertEqual(len(caught), 1)
                    self.assertIn(said, str(caught[0].message))
                else:
                    self.assertEqual(caught, [])
        finally:
            server.shutdown()
            server.server_close()

    def test_arguments_tables_and_pictures(self):
        sent = {}
        data = {
            "picture": {"caption": "The tissue", "image": "data:image/jpeg;base64,/9j/4AAQ"},
            "pairs": [{"a": "x", "b": "y"}],
            "table": {"name": "pairs", "columns": ["a", "z"], "rows": [["x", 1.5], ["y", None]]},
            "genes": {"columns": ["gene", "count"], "abundant": [["SIM001", 9]], "enriched": [["SIM033", 4]]},
        }

        class Recorder(situcraft.SituCraft):
            def call(self, action, **args):
                sent[action] = args
                return situcraft.Result(action, "", data)

        sc = Recorder("http://127.0.0.1:1", "")
        sc.draw_region(name="Nest", class_="Tumor", rectangle=[0, 0, 10, 10])
        self.assertEqual(sent["draw_region"]["class"], "Tumor")
        sc.show_tissue(genes=["SIM001"])
        self.assertIsNone(sent["show_tissue"]["picture"])
        result = sc.show_tissue(file=os.devnull)
        self.assertTrue(sent["show_tissue"]["picture"], "a file asks for the picture")
        self.assertEqual(result.images(), [{"caption": "The tissue", "type": "jpeg", "data": b"\xff\xd8\xff\xe0\x00\x10"}])
        # A table of columns and rows before a list of records; one by its key, or inside another.
        self.assertEqual(result.records(), [{"a": "x", "z": 1.5}, {"a": "y", "z": None}])
        self.assertEqual(result.records("pairs"), [{"a": "x", "b": "y"}])
        self.assertEqual(result.records("genes.enriched"), [{"gene": "SIM033", "count": 4}])
        with self.assertRaises(KeyError):
            result.records("nothing")


if __name__ == "__main__":
    unittest.main()
