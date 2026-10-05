"use strict";

const form = document.getElementById("search-form");
const input = document.getElementById("q");
const suggestionsEl = document.getElementById("suggestions");
const statusEl = document.getElementById("status");
const summaryEl = document.getElementById("summary");
const didYouMeanEl = document.getElementById("did-you-mean");
const list = document.getElementById("stations");

const tempFormat = new Intl.NumberFormat("sv-SE", { minimumFractionDigits: 1, maximumFractionDigits: 1 });
const timeFormat = new Intl.DateTimeFormat("sv-SE", { hour: "2-digit", minute: "2-digit" });

let searchController = null;
let suggestions = [];
let activeIndex = -1;
let suggestTimer = 0;
let suggestController = null;
const suggestCache = new Map();

// ---- Sökning ----

form.addEventListener("submit", (e) => {
  e.preventDefault();
  if (activeIndex >= 0) {
    choose(suggestions[activeIndex]);
    return;
  }
  const q = input.value.trim();
  if (q.length < 2) return;
  closeSuggestions();
  navigate(q);
});

window.addEventListener("popstate", () => loadFromURL());

function navigate(q) {
  const url = new URL(location.href);
  url.searchParams.set("q", q);
  history.pushState({ q }, "", url);
  search(q);
}

function loadFromURL() {
  const q = new URLSearchParams(location.search).get("q") || "";
  input.value = q;
  closeSuggestions();
  if (q.trim().length >= 2) {
    search(q);
  } else {
    clearResults();
  }
}

async function search(q) {
  searchController?.abort();
  searchController = new AbortController();
  clearResults();
  showStatus("Söker…");

  try {
    const res = await fetch(`/api/search?q=${encodeURIComponent(q)}`, { signal: searchController.signal });
    const data = await res.json();
    if (!res.ok) {
      showStatus(data.error || "Något gick fel.", { error: true });
      return;
    }
    render(data);
  } catch (err) {
    if (err.name === "AbortError") return;
    showStatus("Kunde inte nå servern.", { error: true });
  }
}

function render(data) {
  if (!data.place) {
    showStatus(`Hittade ingen ort eller kommun som heter ”${data.query}”.`);
    renderDidYouMean(data.didYouMean);
    return;
  }

  // Visa vilken plats sökningen ledde till, t.ex. "lund" → "Lunds kommun".
  const { place, total } = data;
  input.value = place.label;

  const shown = data.stations.length;
  const count = `${total} ${total === 1 ? "mätpunkt" : "mätpunkter"}`;
  const meta = [
    data.average != null ? `Snitt av ${count}` : count,
    shown < total ? `visar ${shown}` : "",
    `uppdaterat ${timeFormat.format(new Date(data.fetchedAt))}`,
  ];

  document.getElementById("summary-title").textContent = place.label;
  document.getElementById("summary-detail").textContent = place.detail;
  document.getElementById("summary-meta").textContent = meta.filter(Boolean).join(" · ");
  setTemp(document.getElementById("summary-temp"), data.average, "temp temp-lg");
  summaryEl.hidden = false;

  list.replaceChildren(...data.stations.map(stationItem));

  // Kort sammanfattning för skärmläsare; resten finns i listan.
  const avg = data.average != null ? `, snitt ${formatTemp(data.average)}` : "";
  showStatus(`${place.label}: ${count}${avg}.`, { visuallyHidden: true });
}

function renderDidYouMean(places) {
  if (!places?.length) return;
  const buttons = places.map((p) => {
    const b = document.createElement("button");
    b.type = "button";
    b.textContent = p.label;
    b.addEventListener("click", () => {
      input.value = p.label;
      navigate(p.label);
    });
    return b;
  });
  didYouMeanEl.replaceChildren("Menade du ", ...joinWith(buttons, ", ", " eller "), "?");
  didYouMeanEl.hidden = false;
}

function stationItem(s) {
  const li = document.createElement("li");
  const a = document.createElement("a");
  a.href = isTnuURL(s.url) ? s.url : "https://www.temperatur.nu/";
  a.target = "_blank";
  a.rel = "noopener";

  const name = document.createElement("div");
  name.className = "station-name";
  const title = document.createElement("span");
  title.className = "station-title";
  // Tillåt radbrytning efter "/" så att "Jukkasjärvi/Marknadsvägen" bryts
  // mellan orten och platsen i stället för mitt i ett ord.
  s.title.split("/").forEach((part, i) => {
    if (i > 0) title.append("/", document.createElement("wbr"));
    title.append(part);
  });
  const meta = document.createElement("span");
  meta.className = "station-meta";
  meta.textContent = [s.kommun, formatUpdated(s.lastUpdate)].filter(Boolean).join(" · ");
  const newTab = document.createElement("span");
  newTab.className = "visually-hidden";
  newTab.textContent = "(öppnas i ny flik på temperatur.nu)";
  name.append(title, meta, newTab);

  const temp = document.createElement("span");
  setTemp(temp, s.temp, "temp");

  a.append(name, temp);
  li.append(a);
  return li;
}

// setTemp visar en temperatur som en färgad etikett. Färgen är bara ett
// komplement – värdet står alltid i klartext.
function setTemp(el, t, baseClass) {
  if (t == null) {
    el.className = `${baseClass} temp-missing`;
    el.textContent = "–";
    el.title = "Värde saknas";
    const sr = document.createElement("span");
    sr.className = "visually-hidden";
    sr.textContent = "värde saknas";
    el.append(sr);
    return;
  }
  el.className = `${baseClass} temp-${tempLevel(t)}`;
  el.textContent = formatTemp(t);
  el.removeAttribute("title");
}

function tempLevel(t) {
  if (t <= -10) return "frost";
  if (t < 0) return "cold";
  if (t < 10) return "cool";
  if (t < 20) return "mild";
  if (t < 25) return "warm";
  return "hot";
}

function clearResults() {
  summaryEl.hidden = true;
  showStatus("");
  didYouMeanEl.hidden = true;
  didYouMeanEl.replaceChildren();
  list.replaceChildren();
}

function showStatus(text, { error = false, visuallyHidden = false } = {}) {
  statusEl.textContent = text;
  statusEl.hidden = !text;
  statusEl.classList.toggle("error", error);
  statusEl.classList.toggle("visually-hidden", visuallyHidden);
}

// ---- Förslag medan man skriver ----
// Förslagen räknas fram på servern ur en lokal lista med alla mätpunkter,
// så de kostar inga anrop mot temperatur.nu.

input.addEventListener("input", () => {
  clearTimeout(suggestTimer);
  const q = input.value.trim();
  if (q.length < 2) {
    closeSuggestions();
    return;
  }
  suggestTimer = setTimeout(() => fetchSuggestions(q), 120);
});

input.addEventListener("keydown", (e) => {
  if (suggestionsEl.hidden) {
    if (e.key === "ArrowDown" && suggestions.length) openSuggestions();
    return;
  }
  switch (e.key) {
    case "ArrowDown":
      e.preventDefault();
      setActive((activeIndex + 1) % suggestions.length);
      break;
    case "ArrowUp":
      e.preventDefault();
      setActive(activeIndex <= 0 ? suggestions.length - 1 : activeIndex - 1);
      break;
    case "Escape":
      e.preventDefault();
      closeSuggestions();
      break;
    case "Tab":
      closeSuggestions();
      break;
  }
});

input.addEventListener("blur", () => closeSuggestions());

async function fetchSuggestions(q) {
  const key = q.toLowerCase();
  if (suggestCache.has(key)) {
    showSuggestions(suggestCache.get(key));
    return;
  }
  suggestController?.abort();
  suggestController = new AbortController();
  try {
    const res = await fetch(`/api/suggest?q=${encodeURIComponent(q)}`, { signal: suggestController.signal });
    if (!res.ok) return;
    const data = await res.json();
    suggestCache.set(key, data.suggestions);
    // Användaren kan ha skrivit vidare medan svaret var på väg.
    if (input.value.trim().toLowerCase() === key) showSuggestions(data.suggestions);
  } catch {
    // Förslag är en bekvämlighet; vid fel visas helt enkelt inga.
  }
}

function showSuggestions(items) {
  suggestions = items;
  activeIndex = -1;
  input.removeAttribute("aria-activedescendant");
  if (!items.length || document.activeElement !== input) {
    closeSuggestions();
    return;
  }
  suggestionsEl.replaceChildren(...items.map(suggestionItem));
  openSuggestions();
}

function suggestionItem(p, i) {
  const li = document.createElement("li");
  li.id = `suggestion-${i}`;
  li.setAttribute("role", "option");
  li.setAttribute("aria-selected", "false");

  const text = document.createElement("div");
  const label = document.createElement("strong");
  label.textContent = p.label;
  const detail = document.createElement("span");
  detail.className = "detail";
  detail.textContent = p.detail;
  text.append(label, detail);

  const count = document.createElement("span");
  count.className = "count";
  count.textContent = `${p.count} st`;

  li.append(text, count);
  // pointerdown i stället för click så att fältet inte hinner tappa fokus först.
  li.addEventListener("pointerdown", (e) => {
    e.preventDefault();
    choose(p);
  });
  li.addEventListener("pointermove", () => setActive(i));
  return li;
}

function setActive(i) {
  activeIndex = i;
  [...suggestionsEl.children].forEach((li, j) => li.setAttribute("aria-selected", String(j === i)));
  const active = suggestionsEl.children[i];
  if (active) {
    input.setAttribute("aria-activedescendant", active.id);
    active.scrollIntoView({ block: "nearest" });
  }
}

function choose(place) {
  input.value = place.label;
  closeSuggestions();
  navigate(place.label);
}

function openSuggestions() {
  suggestionsEl.hidden = false;
  input.setAttribute("aria-expanded", "true");
}

function closeSuggestions() {
  clearTimeout(suggestTimer);
  suggestionsEl.hidden = true;
  activeIndex = -1;
  input.setAttribute("aria-expanded", "false");
  input.removeAttribute("aria-activedescendant");
}

// ---- Hjälpfunktioner ----

function isTnuURL(u) {
  try {
    const url = new URL(u);
    return url.protocol === "https:" && (url.hostname === "temperatur.nu" || url.hostname.endsWith(".temperatur.nu"));
  } catch {
    return false;
  }
}

function formatTemp(t) {
  return `${tempFormat.format(t)}°`;
}

// lastUpdate kommer som "2026-10-05 18:39:00" i svensk tid.
// Visa bara klockslag om det är från i dag, annars datum och tid.
function formatUpdated(s) {
  if (!s) return "";
  const [date, time = ""] = s.split(" ");
  const hhmm = time.slice(0, 5);
  const today = new Intl.DateTimeFormat("sv-SE", { timeZone: "Europe/Stockholm" }).format(new Date());
  return date === today ? `kl. ${hhmm}` : `${date} ${hhmm}`;
}

// joinWith(["a","b","c"], ", ", " eller ") → ["a", ", ", "b", " eller ", "c"]
function joinWith(nodes, sep, lastSep) {
  return nodes.flatMap((n, i) => (i === 0 ? [n] : [i === nodes.length - 1 ? lastSep : sep, n]));
}

loadFromURL();
