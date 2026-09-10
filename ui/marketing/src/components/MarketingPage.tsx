import { Archive, ArrowRight, Check, FileText, Layers, Search, Shield, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";

const repository = "https://github.com/Aagat/attic";
const setup = `${repository}#run-the-personal-archive`;
const imagePath = (name: string) => `${import.meta.env.BASE_URL}images/${name}.webp`;
const sectionPadding = "px-6 py-16 md:px-10 lg:px-16";

function Eyebrow({ children }: { children: ReactNode }) {
  return <p className="text-[10px] font-semibold tracking-[0.13em] leading-5 uppercase">{children}</p>;
}
function Action({ children, href = setup, dark = false }: { children: ReactNode; href?: string; dark?: boolean }) {
  return <a href={href} className={`inline-flex min-h-[52px] w-fit items-center justify-center gap-3 rounded-[3px] px-7 text-sm font-medium transition-opacity hover:opacity-80 ${dark ? "bg-[var(--ink)] text-[var(--paper)]" : "bg-[var(--accent)] text-[var(--ink)]"}`}>{children}<ArrowRight size={16} aria-hidden="true" /></a>;
}
function Brand() {
  return <a href="#top" aria-label="Attic home" className="inline-flex items-center gap-2.5"><Archive size={20} className="text-[var(--accent)]" aria-hidden="true" /><span className="font-display text-[28px]">attic</span></a>;
}
const features: { title: string; description: string; icon: LucideIcon }[] = [
  { title: "Search the whole archive", description: "Search titles, editable and AI-suggested tags, notes, URLs, captured pages, and PDF text—with filters and ranked snippets.", icon: Search },
  { title: "Preserve every useful version", description: "Keep browser snapshots and earlier captures. If a source fails, Attic can recover an existing public archive copy.", icon: Layers },
  { title: "Make documents worth reading", description: "Clean articles, translate or edit reading editions, verify the generated PDF, then preview, download, or send it to Kindle.", icon: FileText },
];
const steps = [
  ["Capture", "Save the current page, sync Chromium bookmarks, import bookmark HTML, share from Android, or upload a PDF."],
  ["Preserve", "Store versioned snapshots and original PDF bytes. Earlier successful captures remain available when a later attempt fails."],
  ["Read", "Read the clean article, inspect the original layout, translate or edit an edition, preview its PDF, and send it to Kindle."],
];

export function MarketingPage() {
  return <div id="top" className="w-full">
    <a href="#main" className="sr-only focus:not-sr-only focus:absolute focus:z-10 focus:bg-[var(--paper)] focus:p-4">Skip to content</a>
    <header className="flex min-h-[76px] flex-wrap items-center justify-between gap-4 border-b border-[var(--line)] bg-[var(--paper)] px-6 py-4 md:px-14">
      <Brand />
      <nav aria-label="Main navigation" className="flex flex-wrap items-center gap-4 text-[13px] md:gap-[30px]">
        <a className="hover:underline" href="#how-it-works">How it works</a><a className="hover:underline" href="#features">Features</a><a className="hover:underline" href="#self-hosting">Self-hosting</a><a className="rounded-[3px] bg-[var(--accent)] px-5 py-3 font-medium hover:opacity-80" href={setup}>Get Attic</a>
      </nav>
    </header>
    <main id="main">
      <section aria-labelledby="hero-title" className="grid grid-cols-1 lg:min-h-[770px] lg:grid-cols-[610fr_830fr]">
        <div className="flex flex-col justify-between gap-12 bg-[var(--accent-soft)] px-6 py-16 md:px-10 lg:px-16 lg:py-[72px]">
          <div className="flex flex-col gap-7"><div className="flex items-center gap-2.5"><span className="h-1.5 w-1.5 rounded-full bg-[var(--accent)]" /><Eyebrow>Your private reading archive</Eyebrow></div>
            <h1 id="hero-title" className="font-display text-[52px] leading-[0.98] tracking-[-0.025em] md:text-[68px]">The web changes.<br />What you save<br />shouldn’t.</h1>
            <p className="max-w-[482px] text-[17px] leading-[1.55] text-[var(--muted)]">Save bookmarks and PDFs, preserve page snapshots, search the whole collection, and prepare reading documents for Kindle Scribe.</p>
          </div>
          <div className="flex flex-col gap-3.5"><Action>Start your archive</Action><p className="text-xs leading-5 text-[var(--muted)]">Self-hosted • Browser + PWA • Portable backups</p></div>
        </div>
        <img src={imagePath("rRJkS")} width="1660" height="1540" alt="Attic library preview showing search and saved articles and PDFs." className="h-full w-full object-cover" fetchPriority="high" />
      </section>
      <section className="flex flex-col gap-10 border-b border-[var(--line)] bg-[var(--paper)] px-6 py-[46px] md:px-10 lg:min-h-[230px] lg:flex-row lg:items-center lg:gap-14 lg:px-16">
        <div className="flex flex-col gap-2.5 lg:w-[520px] lg:shrink-0"><h2 className="font-display text-4xl">A library, not another feed.</h2><p className="text-sm leading-[1.55] text-[var(--muted)]">Bookmark from the browser, share from mobile, or upload a PDF. Attic keeps the source, its history, and your notes together.</p></div>
        <div className="grid flex-1 gap-5 sm:grid-cols-3">{["Bookmark sync", "Versioned captures", "Kindle-ready PDFs"].map((title, index) => <div key={title} className="border-l border-[var(--line)] pl-4"><span className="text-[11px] text-[var(--muted)]">0{index + 1}</span><h3 className="mt-5 font-display text-[23px] leading-tight">{title}</h3></div>)}</div>
      </section>
      <section id="features" className={`${sectionPadding} flex flex-col gap-[46px] lg:min-h-[970px] lg:py-[84px]`}>
        <div className="flex flex-col justify-between gap-6 md:flex-row md:items-end"><div><Eyebrow>A quiet tool for serious reading</Eyebrow><h2 className="mt-5 font-display text-[42px] leading-[1.05] md:text-[52px]">More than bookmarks.<br />A durable reading system.</h2></div><p className="max-w-[320px] text-sm leading-[1.55] text-[var(--muted)]">Snapshots, original PDFs, notes, tags, reading editions, and portable backups.</p></div>
        <div className="grid flex-1 gap-[18px] md:grid-cols-3">{features.map(({ title, description, icon: Icon }, index) => <article key={title} className={`flex min-h-[360px] flex-col justify-between gap-20 border p-7 ${index === 1 ? "border-[var(--ink)] bg-[var(--accent-soft)]" : "border-[var(--line)] bg-[var(--paper)]"}`}><div><div className={`flex h-[46px] w-[46px] items-center justify-center rounded-[3px] ${index === 1 ? "bg-[var(--paper)]" : "bg-[var(--accent-soft)]"}`}><Icon size={22} aria-hidden="true" /></div><p className="mt-[22px] text-[10px] font-semibold tracking-widest text-[var(--muted)]">0{index + 1}</p></div><div><h3 className="font-display text-[32px] leading-[1.05]">{title}</h3><p className="mt-3.5 text-sm leading-[1.55] text-[var(--muted)]">{description}</p></div></article>)}</div>
      </section>
      <section id="how-it-works" className={`${sectionPadding} grid gap-12 bg-[var(--accent-soft)] lg:min-h-[760px] lg:grid-cols-[500fr_748fr] lg:gap-16 lg:py-[78px]`}>
        <div className="flex flex-col justify-between gap-10"><div><Eyebrow>From open tab to lasting reference</Eyebrow><h2 className="mt-5 font-display text-[44px] leading-[1.05] md:text-[54px]">Save now.<br />Find it years later.</h2><p className="mt-6 text-[15px] leading-[1.55] text-[var(--muted)]">Each stage is durable and independent: saving still works when search, classification, formatting, or delivery is unavailable.</p></div>
          <ol className="space-y-6">{steps.map(([title, description], index) => <li key={title} className="flex gap-5"><span className="pt-1 text-[10px] text-[var(--muted)]">0{index + 1}</span><div><h3 className="font-display text-[22px]">{title}</h3><p className="mt-1 text-xs leading-[1.6] text-[var(--muted)]">{description}</p></div></li>)}</ol>
        </div>
        <img src={imagePath("H093er")} width="1496" height="1208" alt="Browser extension preview: A philosophy of software design saved to Attic, with capture queued." className="h-full w-full object-contain" loading="lazy" />
      </section>
      <section className={`${sectionPadding} grid items-center gap-12 bg-[var(--paper)] lg:min-h-[720px] lg:grid-cols-[700fr_532fr] lg:gap-20 lg:py-[70px]`}>
        <img src={imagePath("C5T8xN")} width="1400" height="1160" alt="Mobile reading preview with a clean article, author details, and comfortable typography." className="w-full" loading="lazy" />
        <div><Eyebrow>Read where you think best</Eyebrow><h2 className="mt-[26px] font-display text-[42px] leading-[1.05] md:text-[50px]">Original, translated,<br />or edited by you.</h2><p className="mt-[26px] text-[15px] leading-[1.55] text-[var(--muted)]">Every saved article keeps its source and capture history. Create a separate reading edition, adjust its cleaned HTML, generate a checked PDF, or deliver the selected edition to Kindle.</p><ul className="my-[26px] space-y-4">{["Original layout and capture history stay available", "Separate editions in 10 supported languages", "Preview, download, or retry Kindle delivery"].map(text => <li key={text} className="flex items-start gap-3 text-[13px] leading-5"><Check size={16} className="shrink-0 text-[var(--accent)]" aria-hidden="true" />{text}</li>)}</ul><Action href={`${repository}#attic`}>Send to Kindle</Action></div>
      </section>
      <section id="self-hosting" className="grid grid-cols-1 lg:min-h-[610px] lg:grid-cols-[650fr_790fr]">
        <div className={`${sectionPadding} flex min-w-0 flex-col justify-between gap-12 bg-[var(--accent-soft)] lg:py-[62px]`}><div><Eyebrow>Self-hosted by design</Eyebrow><h2 className="mt-6 font-display text-[39px] leading-[1.08] md:text-[43px]">Your archive is canonical.<br />Search is rebuildable.</h2><p className="mt-6 text-sm leading-[1.55] text-[var(--muted)]">Run Attic with Docker, PostgreSQL, and a persistent artifact volume. Meilisearch is a disposable projection, while portable ZIP export and restore keep your library movable.</p></div><a href={setup} aria-label="Read setup instructions before running Docker Compose" className="flex items-center gap-4 overflow-x-auto rounded-[3px] border border-[var(--line)] bg-[var(--paper)] p-5 font-mono text-xs hover:border-[var(--ink)]"><span className="text-[var(--accent)]">$</span><code className="whitespace-nowrap">docker compose up -d --build</code><ArrowRight size={16} className="ml-auto shrink-0" aria-hidden="true" /></a></div>
        <div className={`${sectionPadding} flex min-w-0 flex-col justify-between gap-12 bg-[var(--accent)] lg:py-[62px]`}><div><Eyebrow>Make a home for what you read</Eyebrow><h2 className="mt-6 font-display text-[38px] leading-[1.05] sm:text-[44px] md:text-[50px]">Stop losing the things<br />worth remembering.</h2><p className="mt-6 max-w-[510px] text-[15px] leading-[1.55]">Save links and PDFs, preserve their history, and turn the best of them into reading documents you control.</p></div><Action dark>Get started</Action></div>
      </section>
    </main>
    <footer className="flex min-h-[164px] flex-wrap items-center justify-between gap-8 border-t border-[var(--line)] bg-[var(--paper)] px-6 py-10 md:px-16"><div><Brand /><p className="mt-2 text-[11px] leading-5 text-[var(--muted)]">Bookmarks, captures, PDFs, and reading editions—entirely yours.</p></div><nav aria-label="Footer" className="flex items-center gap-[26px] text-xs"><a className="hover:underline" href={`${repository}#readme`}>Documentation</a><a className="hover:underline" href={repository}>GitHub</a><a className="hover:underline" href="#privacy">Privacy</a></nav><p className="text-[11px] text-[var(--muted)]">© {new Date().getFullYear()} Attic</p></footer>
    <details id="privacy" className="border-t border-[var(--line)] px-6 py-5 text-xs text-[var(--muted)] md:px-16"><summary className="w-fit cursor-pointer"><Shield size={14} className="mr-2 inline" aria-hidden="true" />Privacy</summary><p className="mt-3 max-w-[780px] leading-6">This marketing page uses no analytics, cookies, or tracking scripts. Fonts and previews are served with the site. GitHub Pages handles hosting requests. In a self-hosted Attic archive, your configured AI, search, and email services process the content needed for their features. See the <a className="underline" href={`${repository}#readme`}>documentation</a> before configuring your archive.</p></details>
  </div>;
}
