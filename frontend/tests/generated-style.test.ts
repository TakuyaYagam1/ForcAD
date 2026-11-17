import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { formatAllGeneratedClient, formatGeneratedSource } from "../codegen/format-generated.mjs";

test("generated clients keep multiline function bodies after regeneration", async () => {
  const source = [
    "export const select = (value: string) => value.trim();",
    "export function enabled(value: boolean) {return value;}",
    "export function checked(value: boolean) {if (value) return value;}",
  ].join("\n");
  const formatted = await formatGeneratedSource(source);
  assert.ok(formatted.includes("=> {\n  return value.trim()\n}"));
  assert.ok(formatted.includes("enabled(value: boolean) {\n  return value;\n}"));
  assert.ok(formatted.includes("if (value) {\n    return value;\n  }"));
  assert.equal(await formatGeneratedSource(formatted), formatted);
});

test("generated objects and TypeScript members use separate lines", async () => {
  const source = [
    "type Entry = {name: string; value: number};",
    "interface Named {/** retained */ id: string; label: string}",
    "export const entry = {name: 'value', value: 1};",
  ].join("\n");
  const formatted = await formatGeneratedSource(source);
  assert.ok(formatted.includes("type Entry = {\n  name: string;\n  value: number\n}"));
  assert.ok(formatted.includes("/** retained */"));
  assert.ok(formatted.includes("id: string;\n  label: string\n}"));
  assert.ok(formatted.includes("entry = {\n  name: 'value',\n  value: 1\n}"));
  assert.equal(await formatGeneratedSource(formatted), formatted);
});

test("the generation hook formats nested schemas as well as endpoints", async (t) => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "forcad-generated-style-"));
  t.after(async () => {
    await rm(directory, {
      recursive: true,
      force: true
    });
  });
  const schemas = path.join(directory, "schemas");
  const endpoints = path.join(directory, "endpoints");
  await mkdir(schemas);
  await mkdir(endpoints);
  const schema = path.join(schemas, "entry.zod.ts");
  const endpoint = path.join(endpoints, "entry.ts");
  await writeFile(schema, "export const schema = {one: 1, two: 2};\r\n");
  await writeFile(endpoint, "export const request = () => 1;\r\n");
  await formatAllGeneratedClient(directory);
  const schemaSource = await readFile(schema, "utf8");
  const endpointSource = await readFile(endpoint, "utf8");
  assert.ok(schemaSource.includes("one: 1,\n  two: 2"));
  assert.ok(endpointSource.includes("=> {\n  return 1\n}"));
  assert.ok(!schemaSource.includes("\r"));
  assert.ok(!endpointSource.includes("\r"));
  await formatAllGeneratedClient(directory);
  assert.equal(await readFile(schema, "utf8"), schemaSource);
  assert.equal(await readFile(endpoint, "utf8"), endpointSource);
});

test("multiline generated type members receive the same indentation as compact input", async () => {
  const compact = "export type Input = {key: string; nested: {value: number}};\r\n";
  const expanded = [
    "export type Input = {",
    "      key: string;",
    "      nested: {",
    "          value: number",
    "       }",
    "    };",
  ].join("\n");
  const expected = await formatGeneratedSource(compact);
  assert.equal(await formatGeneratedSource(expanded), expected);
  assert.equal(await formatGeneratedSource(expected), expected);
});
