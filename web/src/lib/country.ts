/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

const ISO_ALPHA3_TO_ALPHA2: Record<string, string> = {
  afg: "af", ala: "ax", alb: "al", dza: "dz", asm: "as", and: "ad", ago: "ao", aia: "ai", ata: "aq", atg: "ag", arg: "ar",
  arm: "am", abw: "aw", aus: "au", aut: "at", aze: "az", bhs: "bs", bhr: "bh", bgd: "bd", brb: "bb", blr: "by",
  bel: "be", blz: "bz", ben: "bj", bmu: "bm", btn: "bt", bol: "bo", bes: "bq", bih: "ba", bwa: "bw", bvt: "bv",
  bra: "br", iot: "io", brn: "bn", bgr: "bg", bfa: "bf", bdi: "bi", cpt: "cp", khm: "kh", cmr: "cm", can: "ca",
  cpv: "cv", cym: "ky", caf: "cf", tcd: "td", chl: "cl", chn: "cn", cxr: "cx", cck: "cc", col: "co", com: "km",
  cog: "cg", cod: "cd", cok: "ck", cri: "cr", civ: "ci", hrv: "hr", cub: "cu", cuw: "cw", cyp: "cy", cze: "cz",
  dnk: "dk", dji: "dj", dma: "dm", dom: "do", ecu: "ec", egy: "eg", slv: "sv", gnq: "gq", eri: "er", est: "ee",
  swz: "sz", eth: "et", flk: "fk", fro: "fo", fji: "fj", fin: "fi", fra: "fr", guf: "gf", pyf: "pf", atf: "tf",
  gab: "ga", gmb: "gm", geo: "ge", deu: "de", gha: "gh", gib: "gi", grc: "gr", grl: "gl", grd: "gd", glp: "gp",
  gum: "gu", gtm: "gt", ggy: "gg", gin: "gn", gnb: "gw", guy: "gy", hti: "ht", hmd: "hm", vat: "va", hnd: "hn",
  hkg: "hk", hun: "hu", isl: "is", ind: "in", idn: "id", irn: "ir", irq: "iq", irl: "ie", imn: "im", isr: "il",
  ita: "it", jam: "jm", jpn: "jp", jey: "je", jor: "jo", kaz: "kz", ken: "ke", kir: "ki", prk: "kp", kor: "kr",
  kwt: "kw", kgz: "kg", lao: "la", lva: "lv", lbn: "lb", lso: "ls", lbr: "lr", lby: "ly", lie: "li", ltu: "lt",
  lux: "lu", mac: "mo", mkd: "mk", mdg: "mg", mwi: "mw", mys: "my", mdv: "mv", mli: "ml", mlt: "mt", mhl: "mh",
  mtq: "mq", mrt: "mr", mus: "mu", myt: "yt", mex: "mx", fsm: "fm", mda: "md", mco: "mc", mng: "mn", mne: "me",
  msr: "ms", mar: "ma", moz: "mz", mmr: "mm", nam: "na", nru: "nr", npl: "np", nld: "nl", ncl: "nc", nzl: "nz",
  nic: "ni", ner: "ne", nga: "ng", niu: "nu", nfk: "nf", mnp: "mp", nor: "no", omn: "om", pak: "pk", plw: "pw",
  pse: "ps", pan: "pa", png: "pg", pry: "py", per: "pe", phl: "ph", pcn: "pn", pol: "pl", prt: "pt", pri: "pr",
  qat: "qa", reu: "re", rou: "ro", rus: "ru", rwa: "rw", blm: "bl", shn: "sh", kna: "kn", lca: "lc", maf: "mf",
  spm: "pm", vct: "vc", wsm: "ws", smr: "sm", stp: "st", sau: "sa", sen: "sn", srb: "rs", syc: "sc", sle: "sl",
  sgp: "sg", sxm: "sx", svk: "sk", svn: "si", slb: "sb", som: "so", zaf: "za", sgs: "gs", ssd: "ss", esp: "es",
  lka: "lk", sdn: "sd", sur: "sr", sjm: "sj", swe: "se", che: "ch", syr: "sy", twn: "tw", tjk: "tj", tza: "tz",
  tha: "th", tls: "tl", tgo: "tg", tkl: "tk", ton: "to", tto: "tt", tun: "tn", tur: "tr", tkm: "tm", tca: "tc",
  tuv: "tv", uga: "ug", ukr: "ua", are: "ae", gbr: "gb", usa: "us", umi: "um", ury: "uy", uzb: "uz", vut: "vu",
  ven: "ve", vnm: "vn", vgb: "vg", vir: "vi", wlf: "wf", esh: "eh", yem: "ye", zmb: "zm", zwe: "zw",
}

const VALID_ISO_ALPHA2 = new Set(Object.values(ISO_ALPHA3_TO_ALPHA2).concat(["eu", "un", "xk"]))

const COUNTRY_ALIASES: Record<string, string> = {
  uk: "gb", // United Kingdom -> Great Britain (ISO alpha-2 GB)
  el: "gr", // Greece (EU language code alias -> ISO alpha-2 GR)
  tp: "tl", // Timor-Leste / East Timor (legacy ISO alpha-2 -> TL)
  su: "ru", // Soviet Union (legacy TLD / GeoIP code -> RU)
}

const COUNTRY_NAME_TO_CODE: Record<string, string> = {
  "afghanistan": "af",
  "aland islands": "ax",
  "åland islands": "ax",
  "albania": "al",
  "algeria": "dz",
  "andorra": "ad",
  "angola": "ao",
  "argentina": "ar",
  "armenia": "am",
  "australia": "au",
  "austria": "at",
  "azerbaijan": "az",
  "bahamas": "bs",
  "bahrain": "bh",
  "bangladesh": "bd",
  "barbados": "bb",
  "belarus": "by",
  "belgium": "be",
  "belize": "bz",
  "benin": "bj",
  "bhutan": "bt",
  "bolivia": "bo",
  "bosnia and herzegovina": "ba",
  "botswana": "bw",
  "brazil": "br",
  "brasil": "br",
  "brunei": "bn",
  "bulgaria": "bg",
  "burkina faso": "bf",
  "burundi": "bi",
  "cabo verde": "cv",
  "cambodia": "kh",
  "cameroon": "cm",
  "canada": "ca",
  "cape verde": "cv",
  "chile": "cl",
  "china": "cn",
  "clipperton island": "cp",
  "colombia": "co",
  "costa rica": "cr",
  "croatia": "hr",
  "cuba": "cu",
  "cyprus": "cy",
  "czech republic": "cz",
  "czechia": "cz",
  "denmark": "dk",
  "dominican republic": "do",
  "ecuador": "ec",
  "egypt": "eg",
  "el salvador": "sv",
  "estonia": "ee",
  "ethiopia": "et",
  "finland": "fi",
  "france": "fr",
  "georgia": "ge",
  "germany": "de",
  "deutschland": "de",
  "ghana": "gh",
  "greece": "gr",
  "guatemala": "gt",
  "haiti": "ht",
  "honduras": "hn",
  "hong kong": "hk",
  "hungary": "hu",
  "iceland": "is",
  "india": "in",
  "indonesia": "id",
  "iran": "ir",
  "iraq": "iq",
  "ireland": "ie",
  "israel": "il",
  "italy": "it",
  "italia": "it",
  "jamaica": "jm",
  "japan": "jp",
  "jordan": "jo",
  "kazakhstan": "kz",
  "kenya": "ke",
  "south korea": "kr",
  "korea, republic of": "kr",
  "republic of korea": "kr",
  "north korea": "kp",
  "kuwait": "kw",
  "latvia": "lv",
  "lebanon": "lb",
  "libya": "ly",
  "lithuania": "lt",
  "luxembourg": "lu",
  "malaysia": "my",
  "malta": "mt",
  "mexico": "mx",
  "moldova": "md",
  "monaco": "mc",
  "mongolia": "mn",
  "montenegro": "me",
  "morocco": "ma",
  "nepal": "np",
  "netherlands": "nl",
  "new zealand": "nz",
  "nicaragua": "ni",
  "nigeria": "ng",
  "north macedonia": "mk",
  "norway": "no",
  "oman": "om",
  "pakistan": "pk",
  "palestine": "ps",
  "panama": "pa",
  "paraguay": "py",
  "peru": "pe",
  "philippines": "ph",
  "poland": "pl",
  "portugal": "pt",
  "qatar": "qa",
  "romania": "ro",
  "russia": "ru",
  "russian federation": "ru",
  "saudi arabia": "sa",
  "senegal": "sn",
  "serbia": "rs",
  "singapore": "sg",
  "slovakia": "sk",
  "slovenia": "si",
  "south africa": "za",
  "spain": "es",
  "españa": "es",
  "sri lanka": "lk",
  "sudan": "sd",
  "sweden": "se",
  "switzerland": "ch",
  "syria": "sy",
  "taiwan": "tw",
  "thailand": "th",
  "tunisia": "tn",
  "turkey": "tr",
  "türkiye": "tr",
  "uganda": "ug",
  "ukraine": "ua",
  "united arab emirates": "ae",
  "uae": "ae",
  "united kingdom": "gb",
  "uk": "gb",
  "great britain": "gb",
  "united states": "us",
  "united states of america": "us",
  "usa": "us",
  "uruguay": "uy",
  "uzbekistan": "uz",
  "venezuela": "ve",
  "vietnam": "vn",
  "viet nam": "vn",
  "yemen": "ye",
  "zambia": "zm",
  "zimbabwe": "zw",
}

export function getCountryCode(countryCode?: string, country?: string): string | undefined {
  const candidates = [countryCode, country].filter(Boolean) as string[]

  for (const raw of candidates) {
    const trimmed = raw.trim()
    if (!trimmed) continue

    const lower = trimmed.toLowerCase()

    if (COUNTRY_ALIASES[lower]) {
      return COUNTRY_ALIASES[lower]
    }

    if (lower.length === 2 && VALID_ISO_ALPHA2.has(lower)) {
      return lower
    }

    if (lower.length === 3 && ISO_ALPHA3_TO_ALPHA2[lower]) {
      return ISO_ALPHA3_TO_ALPHA2[lower]
    }

    if (COUNTRY_NAME_TO_CODE[lower]) {
      return COUNTRY_NAME_TO_CODE[lower]
    }
  }

  return undefined
}
