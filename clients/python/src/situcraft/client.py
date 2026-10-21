"""Remote control of a running SituCraft from Python.

SituCraft started with ``--remote-control`` accepts actions from programs on this computer: every
action an AI agent has (open bundles, segment, cluster and label cells, ask which cells meet
conditions, run spatial analyses and cohort comparisons, read results as tables, show the tissue,
write files) is sent as JSON to ``/api/remote/action``. The open SituCraft window performs it and
shows what happens; questions about the library (``describe_project``, ``get_result``,
``query_cells``) are answered by the program itself. Each change goes into the project's history,
and every call into the library's log. Adapted from MaveScape 0.8.0's client (the same author's).

>>> import situcraft
>>> sc = situcraft.connect()                         # finds the running SituCraft
>>> sc.open_example("simulated")
>>> cells = sc.query_cells({"attribute": "transcripts", "min": 50}, limit=0)
>>> cells.frame("rows")                              # the cells as a data frame (pandas)
>>> sc.show_tissue(file="tissue.jpg")                # the tissue as a picture
"""

from __future__ import annotations

import base64
import json
import os
import re
import sys
import urllib.error
import urllib.request
import warnings
from pathlib import Path
from typing import Any

from ._tools import API_VERSION, Tools

DEFAULT_URL = "http://127.0.0.1:8840"
TOKEN_HEADER = "X-SituCraft-Token"
PICTURE = re.compile(r"^data:image/(png|jpeg);base64,")


class SituCraftError(Exception):
    """An action SituCraft refused or could not perform, or a connection that failed.

    The message is SituCraft's reason. ``status`` is the HTTP status (0 when SituCraft could not
    be reached); ``action`` the action.
    """

    def __init__(self, message: str, status: int = 0, action: str | None = None):
        super().__init__(message)
        self.status = status
        self.action = action


def _is_table(value: Any) -> bool:
    """A table as SituCraft gives one: {"columns": [...], "rows": [[...], ...]}."""
    return isinstance(value, dict) and isinstance(value.get("columns"), list) and isinstance(value.get("rows"), list) and all(isinstance(r, list) for r in value["rows"])


def _is_records(value: Any) -> bool:
    return isinstance(value, list) and bool(value) and all(isinstance(x, dict) for x in value)


class Result:
    """SituCraft's answer to an action: ``message`` (a sentence) and ``data`` (JSON values), and
    ``changed``, what the action added, changed or removed in the project."""

    def __init__(self, action: str, message: str, data: Any, changed: list | None = None):
        self.action = action
        self.message = message
        self.data = data
        self.changed = changed or []

    def __repr__(self) -> str:
        return f"<situcraft.Result {self.action}: {self.message}>"

    def __getitem__(self, key):
        return self.data[key]

    def _find(self, key: str | None) -> tuple[Any, list | None]:
        """The table a key names (dotted for one inside another: "genes.enriched", whose columns
        are its parent's), or the first found: a table ({"columns", "rows"}) before a list of
        records, the data's own fields before those inside them."""
        if key is not None:
            parent, value = None, self.data
            for part in key.split("."):
                if not isinstance(value, dict) or part not in value:
                    raise KeyError(f"{self.action}'s data has no {key}")
                parent, value = value, value[part]
            if isinstance(value, list) and value and all(isinstance(r, list) for r in value) and isinstance(parent, dict) and isinstance(parent.get("columns"), list):
                return value, parent["columns"]
            return value, None
        if _is_records(self.data) or _is_table(self.data):
            return self.data, None
        if not isinstance(self.data, dict):
            raise KeyError(f"{self.action} returned no table")
        level = [self.data]
        while level:
            for wanted in (_is_table, _is_records):
                for value in (v for d in level for v in d.values()):
                    if wanted(value):
                        return value, None
            level = [v for d in level for v in d.values() if isinstance(v, dict)]
        raise KeyError(f"{self.action} returned no table; its data has {', '.join(self.data)}")

    def records(self, key: str | None = None) -> list[dict]:
        """The rows of a table in the data as dictionaries: ``data[key]`` (a dotted key for one
        inside another, such as "genes.enriched"), or the first table found."""
        value, columns = self._find(key)
        if _is_table(value):
            columns, value = value["columns"], value["rows"]
        if columns is not None:
            return [dict(zip(columns, row)) for row in value]
        if not _is_records(value) and value != []:
            raise KeyError(f"{self.action}'s {key or 'data'} is not a table")
        return value

    def frame(self, key: str | None = None):
        """The table as a pandas DataFrame (pandas must be installed), its columns in SituCraft's
        order; null values are NaN or None."""
        try:
            import pandas
        except ImportError as error:
            raise ImportError("Result.frame() needs pandas; Result.records() gives the rows as dictionaries.") from error
        value, columns = self._find(key)
        if _is_table(value):
            return pandas.DataFrame(value["rows"], columns=value["columns"])
        if columns is not None:
            return pandas.DataFrame(value, columns=columns)
        return pandas.DataFrame.from_records(self.records(key))

    def images(self) -> list[dict]:
        """The pictures in the data (show_tissue, show_map and figure with picture=True), in order:
        [{"caption", "type" ("png" or "jpeg"), "data" (bytes)}]."""
        found: list[dict] = []

        def walk(value: Any, caption: str) -> None:
            if isinstance(value, dict):
                own = next((value[k] for k in ("caption", "title", "name") if isinstance(value.get(k), str) and value.get(k)), caption)
                for item in value.values():
                    walk(item, own)
            elif isinstance(value, list):
                for item in value:
                    walk(item, caption)
            elif isinstance(value, str):
                match = PICTURE.match(value)
                if match:
                    found.append({"caption": caption, "type": match.group(1), "data": base64.b64decode(value[match.end():])})

        walk(self.data, "")
        return found

    def save_image(self, path: str | os.PathLike, index: int | None = None) -> Path:
        """Writes a picture of the result to a file (``index``: which one, when there are several,
        from 0): the tissue's a JPEG, a map's and a figure's a PNG. Returns its path."""
        found = self.images()
        if not found:
            raise SituCraftError(f"{self.action} returned no picture (show_tissue, show_map and figure give one with picture=True)", action=self.action)
        if index is None and len(found) > 1:
            raise SituCraftError(f"{self.action} returned {len(found)} pictures: give index (from 0)", action=self.action)
        path = Path(path)
        path.write_bytes(found[index or 0]["data"])
        return path


def default_data_dir() -> Path:
    """SituCraft's data folder when it is started without --data-dir (Go's os.UserConfigDir)."""
    home = Path.home()
    if sys.platform == "win32":
        base = os.environ.get("APPDATA")
        return Path(base) / "SituCraft" if base else home / ".situcraft"
    if sys.platform == "darwin":
        return home / "Library" / "Application Support" / "SituCraft"
    base = os.environ.get("XDG_CONFIG_HOME") or str(home / ".config")
    return Path(base) / "SituCraft"


def find_connection(data_dir: str | os.PathLike | None = None) -> dict:
    """Where the running SituCraft is: {"url", "token"}.

    From the environment (SITUCRAFT_URL and SITUCRAFT_TOKEN), else from the remote.json that
    SituCraft writes to its data folder (``data_dir``, SITUCRAFT_DATA_DIR or the default folder),
    else SituCraft's default address without a token.
    """
    url = os.environ.get("SITUCRAFT_URL")
    token = os.environ.get("SITUCRAFT_TOKEN")
    if url:
        return {"url": url, "token": token}
    folder = Path(data_dir or os.environ.get("SITUCRAFT_DATA_DIR") or default_data_dir())
    try:
        info = json.loads((folder / "remote.json").read_text(encoding="utf-8"))
        return {"url": info["url"], "token": token or info.get("token")}
    except (OSError, ValueError, KeyError):
        return {"url": DEFAULT_URL, "token": token}


class SituCraft(Tools):
    """A connection to a running SituCraft; each action is a method (see ``tools()``).

    url, token: SituCraft's address and the token it printed at startup (needed to open and write
    files); found by ``find_connection`` when not given. client: the name SituCraft's history and
    log of calls show for your actions. timeout: seconds to wait for an action (a refinement or a
    cohort's steps take a while).
    """

    def __init__(self, url: str | None = None, token: str | None = None, *, client: str = "Python", timeout: float = 3600, data_dir: str | os.PathLike | None = None):
        found = find_connection(data_dir) if url is None or token is None else {}
        self.url = (url or found.get("url") or DEFAULT_URL).rstrip("/")
        self.token = token if token is not None else found.get("token")
        self.client = client
        self.timeout = timeout
        # The version of the running SituCraft's actions, once connect() has asked.
        self.api: int | None = None

    def __repr__(self) -> str:
        return f"<situcraft.SituCraft {self.url}{'' if self.token else ' (no token)'}>"

    def _request(self, method: str, path: str, body: bytes | None = None, action: str | None = None) -> Any:
        headers = {"Accept": "application/json"}
        if body is not None:
            headers["Content-Type"] = "application/json"
        if self.token:
            headers[TOKEN_HEADER] = self.token
        request = urllib.request.Request(self.url + path, data=body, method=method, headers=headers)
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                return json.loads(response.read().decode("utf-8"))
        except urllib.error.HTTPError as error:
            with error:
                text = error.read().decode("utf-8", "replace")
            try:
                message = json.loads(text).get("error") or text
            except ValueError:
                message = text or error.reason
            if error.code == 404 and path.startswith("/api/remote/"):
                message = f"{self.url} does not accept actions: start SituCraft with --remote-control."
            raise SituCraftError(message, error.code, action) from None
        except urllib.error.URLError as error:
            raise SituCraftError(f"SituCraft is not running at {self.url} ({error.reason}). Start it with: situcraft --remote-control", 0, action) from None

    def call(self, action: str, **args: Any) -> Result:
        """Performs any action by its name, with its arguments as SituCraft names them; arguments
        that are None are left out. Raises SituCraftError, with SituCraft's reason, when the action
        is refused."""
        body = json.dumps({"action": action, "args": {k: v for k, v in args.items() if v is not None}, "client": self.client}).encode("utf-8")
        answer = self._request("POST", "/api/remote/action", body, action)
        if not answer.get("ok"):
            raise SituCraftError(answer.get("message") or answer.get("error") or f"{action} failed", 200, action)
        return Result(action, answer.get("message", ""), answer.get("data"), answer.get("changed"))

    def tools(self) -> list[dict]:
        """The actions this SituCraft performs: name, title, description and argument schema."""
        return self._request("GET", "/api/remote/tools")["tools"]

    def version(self) -> str:
        """The version of the running SituCraft."""
        return self._request("GET", "/api/remote/tools")["version"]


def connect(url: str | None = None, token: str | None = None, **options: Any) -> SituCraft:
    """Connects to the running SituCraft (see ``SituCraft``) and checks that it answers, warning
    when its actions are of another version than this client's (API_VERSION)."""
    sc = SituCraft(url, token, **options)
    answer = sc._request("GET", "/api/remote/tools")
    sc.api = answer.get("api")
    if sc.api is not None and sc.api != API_VERSION:
        newer = sc.api > API_VERSION
        warnings.warn(
            f"SituCraft {answer.get('version')} has version {sc.api} of its actions; this client was made for version {API_VERSION}: "
            f"update {'the client' if newer else 'SituCraft'}. An action one of them lacks fails with SituCraft's reason.",
            stacklevel=2,
        )
    return sc
