import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "@fontsource/inter/400.css";
import "@fontsource/inter/500.css";
import "@fontsource/inter/600.css";
import "@fontsource/newsreader/400.css";
import "./styles.css";
import { MarketingPage } from "./components/MarketingPage";

createRoot(document.getElementById("root")!).render(<StrictMode><MarketingPage /></StrictMode>);
