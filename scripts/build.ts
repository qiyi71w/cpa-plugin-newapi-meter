// Build entry points (run with bun):
//   bun scripts/build.ts web     → internal/webui/dist/index.html (one self-contained page)
//   bun scripts/build.ts plugin  → web, then dist/<goos>/<goarch>/newapi-meter.<so|dll|dylib>
//                                  (the same layout as CPA's plugins/<goos>/<goarch>/ directory)
// Environment: GOOS/GOARCH select the target (default: this machine),
// VERSION sets the plugin version (default 0.0.0-dev), GO overrides the go binary.
import { mkdirSync } from "node:fs";

const mode = process.argv[2] ?? "plugin";

async function buildWeb(): Promise<void> {
  const js = await Bun.build({ entrypoints: ["web/src/main.tsx"], target: "browser", format: "iife", minify: true });
  const css = await Bun.build({ entrypoints: ["web/src/styles.css"], minify: true });
  for (const r of [js, css]) {
    if (!r.success) {
      for (const log of r.logs) console.error(log);
      process.exit(1);
    }
  }
  // Inline content must not close its own element early.
  const script = (await js.outputs[0].text()).replaceAll("</script", "<\\/script");
  const style = (await css.outputs[0].text()).replaceAll("</style", "<\\/style");
  const html =
    `<!doctype html>\n<html lang="zh-CN"><head><meta charset="utf-8">` +
    `<meta name="viewport" content="width=device-width, initial-scale=1">` +
    `<meta name="referrer" content="no-referrer"><title>NewAPI Meter</title>` +
    `<style>${style}</style></head><body><div id="app"></div><script>${script}</script></body></html>\n`;
  mkdirSync("internal/webui/dist", { recursive: true });
  await Bun.write("internal/webui/dist/index.html", html);
  console.log(`web: internal/webui/dist/index.html (${html.length} bytes)`);
}

function goTarget(): { goos: string; goarch: string } {
  const goos = process.env.GOOS || ({ win32: "windows", darwin: "darwin", linux: "linux" } as Record<string, string>)[process.platform];
  const goarch = process.env.GOARCH || ({ x64: "amd64", arm64: "arm64" } as Record<string, string>)[process.arch];
  if (!goos || !goarch) throw new Error(`unsupported build host ${process.platform}/${process.arch}; set GOOS and GOARCH`);
  return { goos, goarch };
}

async function buildPlugin(): Promise<void> {
  const { goos, goarch } = goTarget();
  const ext = goos === "windows" ? "dll" : goos === "darwin" ? "dylib" : "so";
  const dir = `dist/${goos}/${goarch}`;
  const out = `${dir}/newapi-meter.${ext}`;
  mkdirSync(dir, { recursive: true });
  const version = process.env.VERSION || "0.0.0-dev";
  const proc = Bun.spawn(
    [
      process.env.GO || "go",
      "build",
      "-trimpath",
      "-buildmode=c-shared",
      "-ldflags",
      `-s -w -X github.com/qiyi71w/cpa-plugin-newapi-meter/internal/plugin.Version=${version}`,
      "-o",
      out,
      "./cmd/newapi-meter",
    ],
    { stdio: ["inherit", "inherit", "inherit"], env: { ...process.env, CGO_ENABLED: "1", GOOS: goos, GOARCH: goarch } },
  );
  if ((await proc.exited) !== 0) process.exit(1);
  console.log(`plugin: ${out}`);
}

if (mode === "web") {
  await buildWeb();
} else if (mode === "plugin") {
  await buildWeb();
  await buildPlugin();
} else {
  console.error("usage: bun scripts/build.ts [web|plugin]");
  process.exit(2);
}
