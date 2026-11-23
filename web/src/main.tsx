import { render } from "preact";
import { App } from "./app";
import { lang } from "./i18n";
import { startTheme } from "./theme";

document.documentElement.lang = lang === "zh" ? "zh-CN" : "en";
startTheme();
render(<App />, document.getElementById("app")!);
