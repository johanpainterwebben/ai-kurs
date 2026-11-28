"use strict";

// Webbsidans logik:
//   1. Sökning: formuläret eller adressen (?q=) → search() → /api/search → render().
//   2. Förslag medan man skriver: input → fetchSuggestions() → /api/suggest → listan
//      under sökfältet (en combobox).
//   3. Gubben klär sig efter "Känns som", bakgrunden följer den uppmätta temperaturen.
// Temperaturer och "Känns som" räknas ut på servern. Allt som kommer från servern
// eller besökaren sätts med textContent, aldrig som HTML.

const form = document.getElementById("search-form");
const input = document.getElementById("q");
const clearButton = document.getElementById("clear");
const suggestionsEl = document.getElementById("suggestions");
const statusEl = document.getElementById("status");
const summaryEl = document.getElementById("summary");
const staleEl = document.getElementById("stale-notice");
const figureEl = document.getElementById("figure");
const figureTitle = document.getElementById("figure-title");
const didYouMeanEl = document.getElementById("did-you-mean");
const list = document.getElementById("stations");

const tempFormat = new Intl.NumberFormat("sv-SE", { minimumFractionDigits: 1, maximumFractionDigits: 1 });
const timeFormat = new Intl.DateTimeFormat("sv-SE", { hour: "2-digit", minute: "2-digit" });
const dateFormat = new Intl.DateTimeFormat("sv-SE", { day: "numeric", month: "short" });
const stockholmDate = new Intl.DateTimeFormat("sv-SE", { timeZone: "Europe/Stockholm" });
const stockholmTime = new Intl.DateTimeFormat("sv-SE", { timeZone: "Europe/Stockholm", hour: "2-digit", minute: "2-digit" });

let searchController = null;
let suggestions = [];
let activeIndex = -1;
let suggestTimer = 0;
let suggestController = null;
const suggestCache = new Map();
const SUGGEST_CACHE_MAX = 200;
const SEARCH_TIMEOUT_MS = 10_000;

const MSG_TIMEOUT = "Servern svarade inte i tid. Försök igen om en stund.";
const MSG_OFFLINE = "Kunde inte nå servern. Kontrollera internetanslutningen och försök igen.";
const MSG_SERVER_ERROR = "Servern kunde inte svara just nu. Försök igen om en stund.";

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
  setQuery(q);
  closeSuggestions();
  if (q.trim().length >= 2) {
    search(q);
  } else {
    clearResults();
    showWeather(null);
  }
}

async function search(q) {
  searchController?.abort();
  const controller = new AbortController();
  searchController = controller;
  clearResults();
  showStatus("Söker…");

  const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(SEARCH_TIMEOUT_MS)]);
  try {
    const { ok, data } = await fetchJSON(`/api/search?q=${encodeURIComponent(q)}`, signal);
    if (controller !== searchController) return; // en nyare sökning har tagit över
    if (ok && data) {
      render(data);
    } else {
      showStatus(data?.error || MSG_SERVER_ERROR, { error: true });
    }
  } catch (err) {
    if (controller !== searchController || err.name === "AbortError") return;
    showStatus(err.name === "TimeoutError" ? MSG_TIMEOUT : MSG_OFFLINE, { error: true });
  }
}

// fetchJSON hämtar ett svar och tolkar det som JSON. En felsida som inte är
// JSON (t.ex. HTML från en proxy vid 502) ger data = null i stället för ett
// undantag, så att den inte misstas för att servern inte gick att nå.
async function fetchJSON(url, signal) {
  const res = await fetch(url, { signal });
  const isJSON = res.headers.get("Content-Type")?.startsWith("application/json");
  const data = isJSON ? await res.json().catch(nullUnlessAborted) : null;
  return { ok: res.ok, data };
}

function nullUnlessAborted(err) {
  if (err.name === "AbortError" || err.name === "TimeoutError") throw err;
  return null;
}

function render(data) {
  if (!data.place) {
    renderNoPlace(data);
    return;
  }
  // Visa vilken plats sökningen ledde till, t.ex. "lund" → "Lunds kommun"
  // eller "tre högars" → "Lund/Tre Högars väg".
  setQuery(data.place.query);
  const shown = shownValues(data);
  renderStaleNotice(data);
  renderSummary(data.place, shown);
  if (data.total === 1) {
    renderSingleStation(data.stations[0]);
  } else {
    renderStationList(data);
  }
  showWeather(shown.temp, shown.feelsLike);
  announce(data, shown);
}

function renderNoPlace(data) {
  const sameName = (p) => p.label.localeCompare(data.query, "sv", { sensitivity: "accent" }) === 0;
  const ambiguous = data.didYouMean.length > 1 && data.didYouMean.every(sameName);
  showStatus(ambiguous
    ? `Det finns flera platser som heter ”${data.query}”.`
    : `Hittade ingen ort eller kommun som heter ”${data.query}”.`);
  renderDidYouMean(data.didYouMean);
}

// shownValues är det sammanfattningen visar: mätpunktens egna värden när
// platsen bara har en, annars snittet för hela platsen.
function shownValues(data) {
  if (data.total === 1) {
    const st = data.stations[0];
    return { temp: st.temp, feelsLike: st.feelsLike };
  }
  return { temp: data.average, feelsLike: data.averageFeelsLike };
}

function renderStaleNotice(data) {
  if (data.stale) {
    document.getElementById("stale-text").textContent =
      `Det har varit problem att hämta ny data från temperatur.nu. Det som visas hämtades ${formatFetched(data.fetchedAt)}.`;
  }
  staleEl.hidden = !data.stale;
}

function renderSummary(place, shown) {
  document.getElementById("summary-title").textContent = place.label;
  document.getElementById("summary-detail").textContent = place.detail;
  setTemp(document.getElementById("summary-temp"), shown.temp, "temp temp-lg");
  setFeelsLike(document.getElementById("summary-feels"), shown.feelsLike);
  summaryEl.dataset.level = shown.temp == null ? "none" : tempLevel(shown.temp);
  summaryEl.hidden = false;
}

// En enda mätpunkt visas bara i kortet, under det namn man valde, i stället
// för samma träff en gång till i en lista.
function renderSingleStation(st) {
  document.getElementById("summary-meta").textContent = `Mätt ${formatUpdated(st.measuredAt)}`;
  const link = document.getElementById("summary-link");
  link.href = isTnuURL(st.url) ? st.url : "https://www.temperatur.nu/";
  link.hidden = false;
  list.replaceChildren();
}

function renderStationList(data) {
  const shownCount = data.stations.length;
  const meta = [
    data.average != null ? `Snitt av ${countText(data.total)}` : countText(data.total),
    shownCount < data.total ? `visar ${shownCount}` : "",
    `uppdaterat ${formatFetched(data.fetchedAt)}`,
  ];
  document.getElementById("summary-meta").textContent = meta.filter(Boolean).join(" · ");
  document.getElementById("summary-link").hidden = true;
  list.replaceChildren(...data.stations.map(stationItem));
}

function countText(total) {
  return `${total} ${total === 1 ? "mätpunkt" : "mätpunkter"}`;
}

// announce läser upp resultatet för skärmläsare via den dolda statusraden,
// eftersom kortet och listan byts ut utan att fokus flyttas.
function announce(data, shown) {
  const feelsText = shown.feelsLike == null ? "" : `, ${feelsLikeText(shown.feelsLike).toLowerCase()}`;
  const tempText = shown.temp == null ? "värde saknas" : `${formatTemp(shown.temp)}${feelsText}`;
  const spoken = data.total === 1 ? tempText : `${countText(data.total)}, ${shown.temp == null ? tempText : `snitt ${tempText}`}`;
  const staleNotice = data.stale ? ` Obs: datan kan vara inaktuell, den hämtades ${formatFetched(data.fetchedAt)}.` : "";
  showStatus(`${data.place.label}: ${spoken}.${staleNotice}`, { visuallyHidden: true });
}

function renderDidYouMean(places) {
  if (!places?.length) return;
  const buttons = places.map((p) => {
    const b = document.createElement("button");
    b.type = "button";
    // Platser som "Centrum" finns i många orter; visa vilken.
    b.textContent = p.kind === "plats" ? `${p.label} (${p.detail})` : p.label;
    b.addEventListener("click", () => {
      setQuery(p.query);
      navigate(p.query);
    });
    return b;
  });
  didYouMeanEl.replaceChildren("Menade du ", ...joinWith(buttons, ", ", " eller "), "?");
  didYouMeanEl.hidden = false;
}

function stationItem(s) {
  const li = document.createElement("li");
  if (s.temp == null) li.className = "no-value";
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
  meta.textContent = [s.kommun, formatUpdated(s.measuredAt)].filter(Boolean).join(" · ");
  const newTab = document.createElement("span");
  newTab.className = "visually-hidden";
  newTab.textContent = "(öppnas i ny flik på temperatur.nu)";
  const feels = document.createElement("span");
  setFeelsLike(feels, s.feelsLike);
  name.append(title, meta, feels, newTab);

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
    el.textContent = "Saknas";
    el.title = "Mätpunkten har inget aktuellt värde";
    return;
  }
  el.className = `${baseClass} temp-${tempLevel(t)}`;
  el.textContent = formatTemp(t);
  el.removeAttribute("title");
}

// "Känns som" räknas ut av servern med vinden inräknad. Den saknas bara när
// mätpunkten saknar temperatur, och då visas ingenting.
function setFeelsLike(el, t) {
  el.className = "feels-like";
  el.textContent = t == null ? "" : feelsLikeText(t);
  el.hidden = t == null;
}

function feelsLikeText(t) {
  return `Känns som ${formatTemp(t)}`;
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
  staleEl.hidden = true;
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

// ---- Gubben ----
// Varje plagg är ett <g data-item="…"> i SVG:n i index.html.

// Från kallast till varmast; max är övre gräns i °C.
const OUTFITS = [
  { max: -12, items: ["pants", "boots", "puffer", "mittens", "scarf", "beanie", "rosy", "breath", "snow"], text: "dunjacka, halsduk, mössa, vantar och kängor" },
  { max: -4, items: ["pants", "boots", "puffer", "mittens", "scarf", "beanie", "snow"], text: "dunjacka, halsduk, mössa, vantar och kängor" },
  { max: 4, items: ["pants", "boots", "jacket", "scarf", "beanie", "snow"], text: "jacka, halsduk, mössa och kängor" },
  { max: 10, items: ["pants", "shoes", "jacket"], text: "jacka, långbyxor och skor" },
  { max: 15, items: ["pants", "shoes", "hoodie"], text: "huvtröja och långbyxor" },
  { max: 20, items: ["pants", "shoes", "tshirt"], text: "t-shirt och långbyxor" },
  { max: 25, items: ["shorts", "shoes", "tshirt"], text: "t-shirt och shorts" },
  { max: 30, items: ["shorts", "sandals", "tank", "cap", "sunglasses", "sun"], text: "linne, shorts, keps och solglasögon" },
  { max: 35, items: ["shorts", "sandals", "tank", "sunhat", "sunglasses", "sweat", "sun"], text: "linne, shorts, solhatt och solglasögon – och svettas" },
  { max: Infinity, items: ["shorts", "sandals", "tank", "sunhat", "sunglasses", "sweat", "sweat-more", "rosy", "sun"], text: "linne, shorts, solhatt och solglasögon – och svettas mycket" },
];

// Utgångsklädseln innan man sökt eller när värde saknas.
const NEUTRAL_TEMP = 17;

// Bakgrunden följer den uppmätta temperaturen, men gubben klär sig efter
// hur kallt det känns.
function showWeather(t, feelsLike = t) {
  dressFigure(feelsLike);
  if (t == null) {
    delete document.documentElement.dataset.climate;
  } else {
    document.documentElement.dataset.climate = tempLevel(t);
  }
}

function dressFigure(t) {
  const outfit = OUTFITS.find((o) => (t ?? NEUTRAL_TEMP) <= o.max);
  for (const g of figureEl.querySelectorAll("[data-item]")) {
    g.toggleAttribute("data-off", !outfit.items.includes(g.dataset.item));
  }
  figureTitle.textContent = t == null
    ? `Gubbe i ${outfit.text} på väg ut genom dörren`
    : `Gubbe på väg ut, klädd för att det känns som ${formatTemp(t)}: ${outfit.text}`;
}

// ---- Förslag medan man skriver ----

function setQuery(value) {
  input.value = value;
  clearButton.hidden = value === "";
}

clearButton.addEventListener("click", () => {
  setQuery("");
  closeSuggestions();
  input.focus();
});

input.addEventListener("input", () => {
  clearButton.hidden = input.value === "";
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
    const { ok, data } = await fetchJSON(`/api/suggest?q=${encodeURIComponent(q)}`, suggestController.signal);
    if (!ok || !data) return;
    if (suggestCache.size >= SUGGEST_CACHE_MAX) {
      suggestCache.delete(suggestCache.keys().next().value); // äldsta först
    }
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
  setQuery(place.query);
  closeSuggestions();
  navigate(place.query);
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

function formatFetched(iso) {
  const d = new Date(iso);
  const sameDay = d.toDateString() === new Date().toDateString();
  const time = `kl. ${timeFormat.format(d)}`;
  return sameDay ? time : `${dateFormat.format(d)} ${time}`;
}

function formatTemp(t) {
  return `${tempFormat.format(t)}°`;
}

// measuredAt kommer som ISO-tid i UTC ("2026-10-05T16:39:00Z"). Visa den i
// svensk tid, och bara klockslag om den är från i dag, annars datum och tid.
function formatUpdated(iso) {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const date = stockholmDate.format(d);
  const hhmm = stockholmTime.format(d);
  return date === stockholmDate.format(new Date()) ? `kl. ${hhmm}` : `${date} ${hhmm}`;
}

function joinWith(nodes, sep, lastSep) {
  return nodes.flatMap((n, i) => (i === 0 ? [n] : [i === nodes.length - 1 ? lastSep : sep, n]));
}

loadFromURL();
