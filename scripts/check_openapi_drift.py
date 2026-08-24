#!/usr/bin/env python
"""OpenAPI drift check (ADR-0005, E2) — a required CI gate, not a pytest case.

Compares the hand-authored openapi/spec.yaml against the FastAPI app's own
runtime-introspected OpenAPI document (app.openapi(), no server or database needed) and fails
on divergence in the parts that actually describe the contract: which paths/methods exist, which
response status codes each operation declares, and each named schema's required fields and
property names. Deliberately does not diff incidental metadata (auto-generated titles, wording of
descriptions) that FastAPI adds and the hand-authored spec has no reason to match verbatim.
"""

from __future__ import annotations

import sys
from pathlib import Path

import yaml

REPO_ROOT = Path(__file__).resolve().parent.parent
SPEC_PATH = REPO_ROOT / "openapi" / "spec.yaml"


def _paths_and_methods(spec: dict[str, object]) -> set[tuple[str, str]]:
    paths = spec.get("paths", {})
    assert isinstance(paths, dict)
    return {
        (path, method.lower())
        for path, operations in paths.items()
        for method in operations
        if method.lower() in {"get", "post", "put", "patch", "delete"}
    }


def _response_codes(spec: dict[str, object], path: str, method: str) -> set[str]:
    paths = spec["paths"]
    assert isinstance(paths, dict)
    responses = paths[path][method]["responses"]
    assert isinstance(responses, dict)
    return set(responses.keys())


def _schema_shape(spec: dict[str, object], name: str) -> tuple[frozenset[str], frozenset[str]]:
    components = spec.get("components", {})
    assert isinstance(components, dict)
    schemas = components.get("schemas", {})
    assert isinstance(schemas, dict)
    schema = schemas.get(name)
    if schema is None:
        return frozenset(), frozenset()
    properties = frozenset(schema.get("properties", {}).keys())
    required = frozenset(schema.get("required", []))
    return properties, required


def main() -> int:
    from wallet_transfer.handlers.app import create_app

    hand_authored = yaml.safe_load(SPEC_PATH.read_text())
    live = create_app().openapi()

    errors: list[str] = []

    hand_authored_paths = _paths_and_methods(hand_authored)
    live_paths = _paths_and_methods(live)
    if hand_authored_paths != live_paths:
        errors.append(
            f"paths/methods differ: spec has {hand_authored_paths - live_paths or '{}'} not in "
            f"the app; app has {live_paths - hand_authored_paths or '{}'} not in the spec"
        )

    for path, method in hand_authored_paths & live_paths:
        hand_authored_codes = _response_codes(hand_authored, path, method)
        live_codes = _response_codes(live, path, method)
        if hand_authored_codes != live_codes:
            errors.append(
                f"{method.upper()} {path} response codes differ: spec has "
                f"{hand_authored_codes - live_codes or '{}'} not in the app; app has "
                f"{live_codes - hand_authored_codes or '{}'} not in the spec"
            )

    schema_names = set(hand_authored.get("components", {}).get("schemas", {})) | set(
        live.get("components", {}).get("schemas", {})
    )
    for name in sorted(schema_names):
        hand_authored_props, hand_authored_required = _schema_shape(hand_authored, name)
        live_props, live_required = _schema_shape(live, name)
        if hand_authored_props != live_props:
            errors.append(
                f"schema {name} properties differ: spec has "
                f"{hand_authored_props - live_props or '{}'} not in the app; app has "
                f"{live_props - hand_authored_props or '{}'} not in the spec"
            )
        if hand_authored_required != live_required:
            errors.append(
                f"schema {name} required fields differ: spec has "
                f"{hand_authored_required - live_required or '{}'} not in the app; app has "
                f"{live_required - hand_authored_required or '{}'} not in the spec"
            )

    if errors:
        print("OpenAPI drift detected between openapi/spec.yaml and the running app:")
        for error in errors:
            print(f"  - {error}")
        return 1

    print("openapi/spec.yaml matches the app's runtime OpenAPI document.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
