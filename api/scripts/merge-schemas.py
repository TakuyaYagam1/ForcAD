#!/usr/bin/env python3
"""Merge domain schema files into the temporary OpenAPI component map."""

import argparse
from pathlib import Path

import yaml


API_DIR = Path(__file__).resolve().parents[1]
SCHEMAS_DIR = API_DIR / "components" / "schemas"
SCHEMAS_FILE = API_DIR / "components" / "schemas.yml"


class UniqueKeyLoader(yaml.SafeLoader):
    pass


def construct_unique_mapping(loader, node, deep=False):
    loader.flatten_mapping(node)
    mapping = {}
    for key_node, value_node in node.value:
        key = loader.construct_object(key_node, deep=deep)
        if key in mapping:
            raise yaml.constructor.ConstructorError(
                "while constructing a mapping",
                node.start_mark,
                f"duplicate key {key!r}",
                key_node.start_mark,
            )
        mapping[key] = loader.construct_object(value_node, deep=deep)
    return mapping


UniqueKeyLoader.add_constructor(
    yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG,
    construct_unique_mapping,
)


def convert_refs(value):
    if isinstance(value, dict):
        result = {}
        for key, child in value.items():
            if (
                key == "$ref"
                and isinstance(child, str)
                and child.startswith("#/schemas/")
            ):
                result[key] = child.replace("#/schemas/", "#/")
            elif key == "mapping" and isinstance(child, dict):
                result[key] = {
                    name: reference.replace("#/schemas/", "#/")
                    if isinstance(reference, str) and reference.startswith("#/schemas/")
                    else reference
                    for name, reference in child.items()
                }
            else:
                result[key] = convert_refs(child)
        return result
    if isinstance(value, list):
        return [convert_refs(item) for item in value]
    return value


def merge_schemas(schema_dir=SCHEMAS_DIR, output=SCHEMAS_FILE):
    if output.exists():
        raise FileExistsError(f"refusing to overwrite existing {output}")

    merged = {}
    sources = sorted(schema_dir.glob("*_schemas.yml"))
    if not sources:
        raise FileNotFoundError(f"no *_schemas.yml files found in {schema_dir}")

    for path in sources:
        content = yaml.load(path.read_text(encoding="utf-8"), Loader=UniqueKeyLoader)
        if not isinstance(content, dict) or not isinstance(
            content.get("schemas"), dict
        ):
            raise ValueError(f"{path} must contain a top-level schemas mapping")

        for name, schema in content["schemas"].items():
            if name in merged:
                raise ValueError(f"duplicate schema name {name!r} in {path}")
            merged[name] = convert_refs(schema)

    output.write_text(
        yaml.safe_dump(merged, sort_keys=False, allow_unicode=True, width=1000),
        encoding="utf-8",
    )
    print(f"Merged {len(merged)} schemas into {output}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--schema-dir", type=Path, default=SCHEMAS_DIR)
    parser.add_argument("--output", type=Path, default=SCHEMAS_FILE)
    args = parser.parse_args()
    merge_schemas(args.schema_dir, args.output)


if __name__ == "__main__":
    main()
