import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const googleUUIDImport = '\t"github.com/google/uuid"\n';

// ogen v1.24.0 emits google/uuid for OpenAPI's uuid format.
function replaceOnce(source, before, after, label) {
  const count = source.split(before).length - 1;
  if (count !== 1) {
    throw new Error(`expected one ${label}, found ${count}`);
  }
  return source.replace(before, after);
}

function replaceMany(source, before, after, label) {
  const count = source.split(before).length - 1;
  if (count === 0 && source.includes(after)) {
    return source;
  }
  if (count === 0) {
    throw new Error(`expected at least one ${label}`);
  }
  return source.replaceAll(before, after);
}

function ensureUUIDImport(source, standardGroupEnd, label) {
  if (source.includes(googleUUIDImport)) {
    source = replaceOnce(source, standardGroupEnd, `${standardGroupEnd.slice(0, -1)}\t"uuid"\n\n`, `${label} standard import group`);
    return replaceOnce(source, googleUUIDImport, "", "google UUID import");
  }
  if (!source.includes('\t"uuid"\n')) {
    throw new Error(`expected ${label} import`);
  }
  return source;
}

async function updateFile(directory, name, update) {
  const filename = path.join(directory, name);
  const source = await readFile(filename, "utf8");
  const updated = update(source);
  await writeFile(filename, updated);
}

async function patchGeneratedUUIDs(directory) {
  await updateFile(directory, "oas_schemas_gen.go", (source) => {
    return ensureUUIDImport(source, '\t"time"\n\n', "UUID");
  });

  await updateFile(directory, "oas_parameters_gen.go", (source) => {
    source = ensureUUIDImport(source, '\t"net/url"\n\n', "UUID");
    return replaceMany(source, "conv.ToUUID(val)", "uuid.Parse(val)", "UUID path decoder");
  });

  await updateFile(directory, "oas_json_gen.go", (source) => {
    source = ensureUUIDImport(source, '\t"strconv"\n\n', "UUID");
    source = replaceMany(source, "json.EncodeUUID(e, unwrapped)", "e.Str(unwrapped.String())", "UUID JSON encoder");
    source = replaceMany(source, "json.DecodeUUID(d)", "decodeUUID(d)", "UUID JSON decoder");
    if (source.includes("func decodeUUID(d *jx.Decoder)")) return source;
    return replaceOnce(source, "// Encode encodes UUID as json.\nfunc (s UUID)", [
        "func decodeUUID(d *jx.Decoder) (uuid.UUID, error) {",
        "\tvalue, err := d.Str()",
        "\tif err != nil {",
        "\t\treturn uuid.UUID{}, err",
        "\t}",
        "\treturn uuid.Parse(string(value))",
        "}",
        "",
        "// Encode encodes UUID as json.\nfunc (s UUID)",
      ].join("\n"),
      "UUID helper insertion point",
    );
  });
}

async function main(args) {
  if (args.length !== 1) {
    throw new Error("usage: node patch-ogen-uuid.mjs <generated-api-directory>");
  }
  await patchGeneratedUUIDs(path.resolve(args[0]));
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main(process.argv.slice(2)).catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
  });
}
