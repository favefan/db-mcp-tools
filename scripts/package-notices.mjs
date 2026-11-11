import { chmod, copyFile, mkdir, readFile, readdir, writeFile } from "node:fs/promises";
import { basename, dirname, isAbsolute, join, relative, resolve } from "node:path";
import { root, hash, pnpm, run } from "./package-lib.mjs";

export async function notices(output, { server, target, client = false, dm = "omit" } = {}) {
  await mkdir(output, { recursive: true });
  const inventory = [];
  const supplemental = new Map(
    JSON.parse(await readFile(join(root, "third-party/notices.json"), "utf8")).map((v) => [
      v.id,
      v,
    ]),
  );
  const add = async (ecosystem, name, version, dir, declared, authors, explicitFile, sourceUrl) => {
    const id = `${ecosystem}-${name}-${version}`.replace(/[^a-zA-Z0-9_.-]/g, "_");
    const destination = join(output, id);
    const candidates = (await readdir(dir, { withFileTypes: true }))
      .filter((v) => v.isFile() && /^(licen[cs]e|copying|notice)([._-]|$)/i.test(v.name))
      .map((v) => join(dir, v.name));
    if (explicitFile) {
      explicitFile = isAbsolute(explicitFile) ? explicitFile : join(dir, explicitFile);
      const within = relative(dir, explicitFile);
      if (within && !within.startsWith("..") && !isAbsolute(within)) candidates.push(explicitFile);
    }
    const supplement = supplemental.get(`${ecosystem}:${name}@${version}`);
    if (!candidates.length && supplement) {
      for (const file of supplement.files) {
        const path = resolve(root, "third-party", file.path);
        const within = relative(join(root, "third-party"), path);
        if (
          !within ||
          within.startsWith("..") ||
          isAbsolute(within) ||
          (await hash(path)) !== file.sha256
        )
          throw new Error(`Invalid supplemental license file: ${file.path}`);
        candidates.push(path);
      }
    }
    const saved = [];
    for (const source of new Set(candidates)) {
      await mkdir(destination, { recursive: true });
      const name = basename(source);
      await copyFile(source, join(destination, name));
      // Go's module cache contains read-only license files. Build resources
      // must remain replaceable when switching or rebuilding package modes.
      await chmod(join(destination, name), 0o644);
      saved.push(`${id}/${name}`);
    }
    inventory.push({
      ecosystem,
      name,
      version,
      declared_license: declared || "see included upstream files",
      authors,
      source_url: sourceUrl,
      license_files: saved,
      license_provenance: supplement?.provenance ?? "upstream package files",
      license_source_urls: supplement?.files.map((v) => v.source_url),
      review_required: saved.length === 0 || supplement?.review_required === true,
    });
  };
  if (server) {
    const modules = [
      ...new Set(
        (
          await run(
            "go",
            [
              "list",
              "-deps",
              ...(dm === "native" ? ["-tags", "dm"] : []),
              "-f",
              "{{if .Module}}{{.Module.Path}}{{end}}",
              "./cmd/dbmcp",
            ],
            { cwd: join(root, "server"), env: server.env, quiet: true },
          )
        )
          .split(/\r?\n/)
          .filter((v) => v && v !== "dbmcp.local/server"),
      ),
    ];
    const rows = await run(
      "go",
      ["list", "-m", "-f", "{{.Path}}\t{{.Version}}\t{{.Dir}}", ...modules],
      { cwd: join(root, "server"), env: server.env, quiet: true },
    );
    for (const row of rows.trim().split(/\r?\n/)) {
      const [name, version, dir] = row.split("\t");
      if (!dir) throw new Error(`Missing module source for license collection: ${name}`);
      const escaped = (v) => v.replace(/[A-Z]/g, (c) => `!${c.toLowerCase()}`);
      await add(
        "go",
        name,
        version,
        dir,
        undefined,
        undefined,
        undefined,
        `https://proxy.golang.org/${escaped(name)}/@v/${escaped(version)}.zip`,
      );
    }
  }
  if (client) {
    const tree = JSON.parse(
      await pnpm(["--dir", "client", "list", "--prod", "--depth", "Infinity", "--json"], {
        quiet: true,
      }),
    );
    const dependencies = new Map();
    function visit(items) {
      for (const [name, item] of Object.entries(items ?? {})) {
        if (!dependencies.has(item.path)) {
          dependencies.set(item.path, { ...item, name });
          visit(item.dependencies);
        }
      }
    }
    tree.forEach((item) => visit(item.dependencies));
    for (const item of dependencies.values()) {
      const pkg = JSON.parse(await readFile(join(item.path, "package.json"), "utf8"));
      await add(
        "npm",
        item.name,
        item.version,
        item.path,
        pkg.license,
        pkg.author,
        undefined,
        item.resolved,
      );
    }
    const metadata = JSON.parse(
      await run(
        "cargo",
        ["metadata", "--format-version", "1", "--locked", "--filter-platform", target],
        { cwd: join(root, "client/src-tauri"), quiet: true },
      ),
    );
    const nodes = new Map(metadata.resolve.nodes.map((node) => [node.id, node]));
    const selected = new Set();
    function walk(id) {
      if (selected.has(id)) return;
      selected.add(id);
      for (const dependency of nodes.get(id)?.deps ?? [])
        if (dependency.dep_kinds.some((v) => v.kind !== "dev")) walk(dependency.pkg);
    }
    walk(metadata.resolve.root);
    for (const pkg of metadata.packages.filter(
      (pkg) => selected.has(pkg.id) && pkg.id !== metadata.resolve.root,
    )) {
      await add(
        "cargo",
        pkg.name,
        pkg.version,
        dirname(pkg.manifest_path),
        pkg.license,
        pkg.authors,
        pkg.license_file,
        `https://crates.io/api/v1/crates/${pkg.name}/${pkg.version}/download`,
      );
    }
  }
  inventory.sort((a, b) => `${a.ecosystem}/${a.name}`.localeCompare(`${b.ecosystem}/${b.name}`));
  await writeFile(join(output, "inventory.json"), JSON.stringify(inventory, null, 2) + "\n");
  await writeFile(
    join(output, "SOURCE-AVAILABILITY.md"),
    "# Component source availability\n\nThe exact upstream source archives are linked in each inventory.json entry. Go modules may also be obtained with go mod download <module>@<version>; Cargo and npm entries use their published registry archives. The project does not modify these dependencies. MPL components remain available in Source Code Form under their upstream license. Project source is supplied in the accompanying repository.\n",
  );
  const review = inventory.filter((v) => v.review_required);
  await writeFile(
    join(output, "THIRD-PARTY-NOTICES.md"),
    `# Third-party components\n\nUpstream license and notice files are retained in this directory. Component versions, declared licenses and source authors are recorded in inventory.json. The project's MIT license does not replace dependency licenses.\n\n${dm === "native" ? "This local build contains the native DM driver. No independent redistribution license was found in that module; public redistribution is not represented as authorized.\n\n" : ""}${review.length ? `Components with unresolved license or notice provenance require review: ${review.map((v) => `${v.name} ${v.version}`).join(", ")}. Declared SPDX identifiers are metadata, not a claim of independent legal verification.\n` : "License text for every selected component is included; provenance and source availability are recorded in inventory.json.\n"}`,
  );
  return {
    components: inventory.length,
    review_required: review.map((v) => `${v.name}@${v.version}`),
  };
}
