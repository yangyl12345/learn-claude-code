import * as fs from "fs";
import * as path from "path";
import { execFileSync } from "child_process";
import type {
  AgentVersion,
  VersionDiff,
  DocContent,
  VersionIndex,
  ChapterImage,
} from "../src/types/agent-data";
import { VERSION_META, VERSION_ORDER, LEARNING_PATH } from "../src/lib/constants";

const WEB_DIR = path.resolve(__dirname, "..");
const REPO_ROOT = path.resolve(WEB_DIR, "..");
const LEGACY_AGENTS_DIR = path.join(REPO_ROOT, "agents");
const LEGACY_DOCS_DIR = path.join(REPO_ROOT, "docs");
const OUT_DIR = path.join(WEB_DIR, "src", "data", "generated");
const PUBLIC_DIR = path.join(WEB_DIR, "public");
const COURSE_ASSETS_DIR = path.join(PUBLIC_DIR, "course-assets");

// 课程入口只负责组装 runtime，工具定义位于 internal/。这张显式映射让
// 教学站仍能展示每一课新增的能力，而不要求重复扫描共享 Go 包。
const GO_LESSON_TOOLS: Record<string, string[]> = {
  s01: ["bash"],
  s02: ["bash", "read_file", "write_file", "edit_file", "glob"],
  s03: ["bash", "read_file", "write_file", "edit_file", "glob", "permission"],
  s04: ["bash", "read_file", "write_file", "edit_file", "glob", "hooks"],
  s05: ["bash", "read_file", "write_file", "edit_file", "glob", "todo_write"],
  s06: ["bash", "read_file", "write_file", "edit_file", "glob", "task"],
  s07: ["bash", "read_file", "write_file", "edit_file", "glob", "load_skill"],
  s08: ["bash", "read_file", "write_file", "edit_file", "glob", "compact"],
  s09: ["bash", "read_file", "write_file", "edit_file", "glob", "memory"],
  s10: ["bash", "read_file", "write_file", "edit_file", "glob", "create_task", "update_task", "list_tasks", "claim_task", "complete_task"],
  s11: ["bash", "read_file", "write_file", "edit_file", "glob", "background"],
  s12: ["bash", "schedule_cron", "list_crons", "cancel_cron"],
  s13: ["bash", "task", "spawn_teammate", "send_message", "request_plan", "review_plan", "request_shutdown", "create_worktree"],
  s14: ["bash", "connect_mcp", "mcp__docs__search", "mcp__docs__get_version"],
  s15: ["bash", "memory", "skills", "tasks", "teams", "cron", "background", "MCP", "hooks", "compact"],
  s16: ["agent", "parallel", "pipeline", "snapshot", "resume"],
  s17: ["bash", "goal", "evaluator"],
};

type Locale = "en" | "zh" | "ja";

interface ChapterSource {
  id: string;
  dirName: string;
  dirPath: string;
  codePath: string;
}

function dirToVersionId(dirName: string): string | null {
  const match = dirName.match(/^(s\d{2})_/);
  return match ? match[1] : null;
}

function filenameToVersionId(filename: string): string | null {
  const base = path.basename(filename, ".py");
  if (base === "s_full" || base === "__init__") return null;

  const match = base.match(/^(s\d+[a-c]?)_/);
  return match ? match[1] : null;
}

function listRootChapters(): ChapterSource[] {
  return fs
    .readdirSync(REPO_ROOT, { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .map((entry) => entry.name)
    .filter((name) => /^s\d{2}_/.test(name))
    .sort()
    .map((dirName) => {
      const id = dirToVersionId(dirName);
      if (!id) return null;
      const dirPath = path.join(REPO_ROOT, dirName);
      const codePath = path.join(dirPath, "main.go");
      if (!fs.existsSync(codePath)) return null;
      return { id, dirName, dirPath, codePath };
    })
    .filter((chapter): chapter is ChapterSource => chapter !== null);
}

function extractClasses(
  lines: string[]
): { name: string; startLine: number; endLine: number }[] {
  const classes: { name: string; startLine: number; endLine: number }[] = [];
  const classPattern = /^type\s+(\w+)\s+struct/;

  for (let i = 0; i < lines.length; i++) {
    const match = lines[i].match(classPattern);
    if (!match) continue;

    const name = match[1];
    const startLine = i + 1;
    let endLine = lines.length;
    for (let j = i + 1; j < lines.length; j++) {
      if (
        lines[j].match(/^type\s+\w+\s+struct/) ||
        lines[j].match(/^func\s/) ||
        (lines[j].match(/^\S/) &&
          lines[j].trim() !== "" &&
          !lines[j].startsWith("#") &&
          !lines[j].startsWith("@"))
      ) {
        endLine = j;
        break;
      }
    }
    classes.push({ name, startLine, endLine });
  }

  return classes;
}

function extractFunctions(
  lines: string[]
): { name: string; signature: string; startLine: number }[] {
  const functions: { name: string; signature: string; startLine: number }[] = [];
  const funcPattern = /^func\s+(?:\([^)]*\)\s+)?(\w+)\s*\((.*?)\)/;

  for (let i = 0; i < lines.length; i++) {
    const match = lines[i].match(funcPattern);
    if (!match) continue;
    functions.push({
      name: match[1],
      signature: `func ${match[1]}(${match[2]})`,
      startLine: i + 1,
    });
  }

  return functions;
}

function assignmentBody(source: string, openIndex: number): string {
  const open = source[openIndex];
  const close = open === "[" ? "]" : "}";
  let depth = 0;
  let quote = "";
  let triple = false;
  let escaped = false;
  let comment = false;

  for (let index = openIndex; index < source.length; index++) {
    const char = source[index];
    const nextThree = source.slice(index, index + 3);
    if (comment) {
      if (char === "\n") comment = false;
      continue;
    }
    if (quote) {
      if (escaped) {
        escaped = false;
        continue;
      }
      if (char === "\\") {
        escaped = true;
        continue;
      }
      if (triple && nextThree === quote.repeat(3)) {
        quote = "";
        triple = false;
        index += 2;
      } else if (!triple && char === quote) {
        quote = "";
      }
      continue;
    }
    if (char === "#") {
      comment = true;
      continue;
    }
    if (char === '"' || char === "'") {
      quote = char;
      triple = nextThree === char.repeat(3);
      if (triple) index += 2;
      continue;
    }
    if (char === open) depth += 1;
    if (char === close) {
      depth -= 1;
      if (depth === 0) return source.slice(openIndex, index + 1);
    }
  }
  return "";
}

function extractTools(source: string): string[] {
  const toolPattern = /(?:Name|name)\s*:\s*"([\w-]+)"|"name"\s*:\s*"([\w-]+)"/g;
  const tools = new Set<string>();
  let tool;
  while ((tool = toolPattern.exec(source)) !== null) {
    tools.add(tool[1] ?? tool[2]);
  }
  return Array.from(tools);
}

function countLoc(lines: string[]): number {
  return lines.filter((line) => {
    const trimmed = line.trim();
    return trimmed !== "" && !trimmed.startsWith("#");
  }).length;
}

function detectLocale(relPath: string): Locale {
  if (relPath.startsWith("zh/") || relPath.startsWith("zh\\")) return "zh";
  if (relPath.startsWith("ja/") || relPath.startsWith("ja\\")) return "ja";
  return "en";
}

function extractDocVersion(filename: string): string | null {
  const match = filename.match(/^(s\d+[a-c]?)-/);
  return match ? match[1] : null;
}

function readText(filePath: string): string {
  return fs.readFileSync(filePath, "utf-8").replace(/\r\n/g, "\n");
}

function titleFromMarkdown(content: string, fallback: string): string {
  const titleMatch = content.match(/^#\s+(.+)$/m);
  return titleMatch ? titleMatch[1] : fallback;
}

function cleanCourseAssets() {
  fs.rmSync(COURSE_ASSETS_DIR, { recursive: true, force: true });
  fs.mkdirSync(COURSE_ASSETS_DIR, { recursive: true });
}

function copyChapterAssets(chapter: ChapterSource): ChapterImage[] {
  const imagesDir = path.join(chapter.dirPath, "images");
  if (!fs.existsSync(imagesDir)) return [];

  const outDir = path.join(COURSE_ASSETS_DIR, chapter.dirName);
  fs.mkdirSync(outDir, { recursive: true });
  fs.cpSync(imagesDir, outDir, { recursive: true });

  return fs
    .readdirSync(imagesDir)
    .filter((filename) => filename.endsWith(".svg"))
    .filter((filename) => !filename.includes(".en.") && !filename.includes(".ja."))
    .sort()
    .map((filename) => ({
      src: `/course-assets/${chapter.dirName}/${filename}`,
      alt: filename.replace(/\.svg$/, "").replace(/-/g, " "),
    }));
}

function localeReadmeName(locale: Locale): string {
  if (locale === "en") return "README.md";
  return `README.${locale}.md`;
}

function rewriteChapterMarkdown(
  content: string,
  chapter: ChapterSource,
  locale: Locale
): string {
  let next = content;

  next = next.replace(
    /^\[English\]\(README\.md\)\s*.\s*\[中文\]\(README\.zh\.md\)\s*.\s*\[日本語\]\(README\.ja\.md\)\n\n?/m,
    ""
  );

  next = next.replace(
    /(!\[[^\]]*\]\()images\/([^)]+)(\))/g,
    `$1/course-assets/${chapter.dirName}/$2$3`
  );

  next = next.replace(
    /\]\(\.\.\/(s\d{2}_[^)\/]+)\/?\)/g,
    (_match, dirName) => {
      const id = dirToVersionId(dirName);
      return id ? `](/${locale}/${id})` : `](../${dirName}/)`;
    }
  );

  next = next.replace(
    /\]\(\.\/(s\d{2}_[^)\/]+)\/?\)/g,
    (_match, dirName) => {
      const id = dirToVersionId(dirName);
      return id ? `](/${locale}/${id})` : `](./${dirName}/)`;
    }
  );

  return next;
}

function buildRootVersions(chapters: ChapterSource[]): AgentVersion[] {
  const versions: AgentVersion[] = [];
  for (const chapter of chapters) {
    const source = readText(chapter.codePath);
    const lines = source.split("\n");
    const meta = VERSION_META[chapter.id];
    const localTools = GO_LESSON_TOOLS[chapter.id] ?? extractTools(source);
    const inheritedId = source.match(/^INHERITS_TOOLS_FROM\s*=\s*"(s\d{2})"/m)?.[1];
    const inheritedTools = inheritedId
      ? versions.find((version) => version.id === inheritedId)?.tools ?? []
      : [];

    versions.push({
      id: chapter.id,
      filename: `${chapter.dirName}/main.go`,
      title: meta?.title ?? chapter.id,
      subtitle: meta?.subtitle ?? "",
      loc: countLoc(lines),
      tools: Array.from(new Set([...inheritedTools, ...localTools])),
      newTools: [] as string[],
      coreAddition: meta?.coreAddition ?? "",
      keyInsight: meta?.keyInsight ?? "",
      // 每个课程入口是 main package，具体 runtime 类型位于 internal/。
      // 用入口声明作为教学站的稳定节点，避免空数组被 TypeScript 推断成 never[]。
      classes: extractClasses(lines).length > 0
        ? extractClasses(lines)
        : [{ name: "GoLessonEntrypoint", startLine: 1, endLine: lines.length }],
      functions: extractFunctions(lines),
      layer: meta?.layer ?? "tools",
      source,
      images: copyChapterAssets(chapter),
    });
  }
  return versions;
}

function buildLegacyVersions(): AgentVersion[] {
  if (!fs.existsSync(LEGACY_AGENTS_DIR)) return [];

  const agentFiles = fs
    .readdirSync(LEGACY_AGENTS_DIR)
    .filter((filename) => filename.startsWith("s") && filename.endsWith(".py"));

  const versions = agentFiles
    .map((filename) => {
      const id = filenameToVersionId(filename);
      if (!id) return null;

      const filePath = path.join(LEGACY_AGENTS_DIR, filename);
      const source = readText(filePath);
      const lines = source.split("\n");
      const meta = VERSION_META[id];

      return {
        id,
        filename,
        title: meta?.title ?? id,
        subtitle: meta?.subtitle ?? "",
        loc: countLoc(lines),
        tools: extractTools(source),
        newTools: [] as string[],
        coreAddition: meta?.coreAddition ?? "",
        keyInsight: meta?.keyInsight ?? "",
        classes: extractClasses(lines),
        functions: extractFunctions(lines),
        layer: meta?.layer ?? "tools",
        source,
        images: [] as ChapterImage[],
      };
    })
    .filter((version): version is AgentVersion => version !== null);

  return versions;
}

function buildRootDocs(chapters: ChapterSource[]): DocContent[] {
  const docs: DocContent[] = [];
  const locales: Locale[] = ["en", "zh", "ja"];

  for (const chapter of chapters) {
    for (const locale of locales) {
      const filename = localeReadmeName(locale);
      const filePath = path.join(chapter.dirPath, filename);
      if (!fs.existsSync(filePath)) continue;

      const raw = readText(filePath);
      const content = rewriteChapterMarkdown(raw, chapter, locale);
      docs.push({
        version: chapter.id,
        locale,
        title: titleFromMarkdown(content, filename),
        content,
      });
    }
  }

  return docs;
}

function buildLegacyDocs(): DocContent[] {
  const docs: DocContent[] = [];
  if (!fs.existsSync(LEGACY_DOCS_DIR)) return docs;

  const localeDirs: Locale[] = ["en", "zh", "ja"];
  for (const locale of localeDirs) {
    const localeDir = path.join(LEGACY_DOCS_DIR, locale);
    if (!fs.existsSync(localeDir)) continue;

    const docFiles = fs.readdirSync(localeDir).filter((f) => f.endsWith(".md"));
    for (const filename of docFiles) {
      const version = extractDocVersion(filename);
      if (!version) continue;

      const relPath = path.join(locale, filename);
      const filePath = path.join(LEGACY_DOCS_DIR, relPath);
      const content = readText(filePath);
      docs.push({
        version,
        locale: detectLocale(relPath),
        title: titleFromMarkdown(content, filename),
        content,
      });
    }
  }

  return docs;
}

function computeNewTools(versions: AgentVersion[]) {
  for (let i = 0; i < versions.length; i++) {
    const prev = i > 0 ? new Set(versions[i - 1].tools) : new Set<string>();
    versions[i].newTools = versions[i].tools.filter((tool) => !prev.has(tool));
  }
}

function buildDiffs(versions: AgentVersion[]): VersionDiff[] {
  const diffs: VersionDiff[] = [];
  const versionMap = new Map(versions.map((version) => [version.id, version]));

  for (let i = 1; i < LEARNING_PATH.length; i++) {
    const fromId = LEARNING_PATH[i - 1];
    const toId = LEARNING_PATH[i];
    const fromVer = versionMap.get(fromId);
    const toVer = versionMap.get(toId);
    if (!fromVer || !toVer) continue;

    const fromClassNames = new Set(fromVer.classes.map((cls) => cls.name));
    const fromFuncNames = new Set(fromVer.functions.map((fn) => fn.name));
    const fromToolNames = new Set(fromVer.tools);

    diffs.push({
      from: fromId,
      to: toId,
      newClasses: toVer.classes
        .map((cls) => cls.name)
        .filter((name) => !fromClassNames.has(name)),
      newFunctions: toVer.functions
        .map((fn) => fn.name)
        .filter((name) => !fromFuncNames.has(name)),
      newTools: toVer.tools.filter((tool) => !fromToolNames.has(tool)),
      locDelta: toVer.loc - fromVer.loc,
    });
  }

  return diffs;
}

function sortVersions(versions: AgentVersion[]) {
  const orderMap = new Map(VERSION_ORDER.map((id, index) => [id, index]));
  versions.sort(
    (a, b) => (orderMap.get(a.id as any) ?? 99) - (orderMap.get(b.id as any) ?? 99)
  );
}

function main() {
  console.log("Extracting course content...");
  console.log(`  Repo root: ${REPO_ROOT}`);

  cleanCourseAssets();

  // Go generator 是课程元数据的权威来源；本脚本继续负责 Markdown、SVG
  // 和前端兼容字段的拼装，以保持现有教学站交互不变。
  execFileSync("go", ["run", "./cmd/coursegen", "--repo-root", REPO_ROOT, "--output", OUT_DIR], {
    cwd: REPO_ROOT,
    stdio: "inherit",
  });

  const rootChapters = listRootChapters();
  const useRootTrack = rootChapters.length > 0;

  console.log(
    useRootTrack
      ? `  Source: root chapter folders (${rootChapters.length})`
      : "  Source: legacy agents/docs folders"
  );

  const versions = useRootTrack
    ? buildRootVersions(rootChapters)
    : buildLegacyVersions();
  const docs = useRootTrack ? buildRootDocs(rootChapters) : buildLegacyDocs();

  sortVersions(versions);
  computeNewTools(versions);
  const diffs = buildDiffs(versions);

  fs.mkdirSync(OUT_DIR, { recursive: true });

  const index: VersionIndex = { versions, diffs };
  fs.writeFileSync(path.join(OUT_DIR, "versions.json"), JSON.stringify(index, null, 2));
  fs.writeFileSync(path.join(OUT_DIR, "docs.json"), JSON.stringify(docs, null, 2));

  console.log("\nExtraction complete:");
  console.log(`  ${versions.length} versions`);
  console.log(`  ${diffs.length} diffs`);
  console.log(`  ${docs.length} docs`);
  for (const version of versions) {
    console.log(
      `    ${version.id}: ${version.loc} LOC, ${version.tools.length} tools, ${version.classes.length} classes, ${version.functions.length} functions`
    );
  }
}

main();
