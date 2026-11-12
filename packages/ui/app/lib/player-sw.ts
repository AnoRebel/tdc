/**
 * The video player's own labels in Swahili.
 *
 * Video.js ships packs for fifty-odd languages and Swahili is not one of them,
 * so without this every control would be read out in English to somebody who
 * chose Kiswahili. Registered once, on import; the player's provider picks the
 * language from the `lang` it is given.
 */
import { registerI18n, type Translations } from "@videojs/html/i18n";

const sw = {
  buttons: {
    play: "Cheza",
    pause: "Sitisha",
    replay: "Cheza tena",
    mute: "Zima sauti",
    unmute: "Washa sauti",
  },
  seek: {
    forward: "Sogeza mbele sekunde {seconds}",
    backward: "Rudisha nyuma sekunde {seconds}",
  },
  fullscreen: { enter: "Skrini nzima", exit: "Toka kwenye skrini nzima" },
  captions: { enable: "Washa manukuu", disable: "Zima manukuu" },
  pip: { enter: "Picha ndani ya picha", exit: "Toka kwenye picha ndani ya picha" },
  live: { playing: "Inacheza mubashara", seekToEdge: "Nenda kwenye mubashara", badge: "Mubashara" },
  cast: { start: "Anza kutuma kwenye kifaa", stop: "Acha kutuma", connecting: "Inaunganisha" },
  airplay: { start: "Anza AirPlay", stop: "Acha AirPlay" },
  slider: { seek: "Mahali pa kucheza" },
  time: {
    current: "Muda wa sasa",
    duration: "Urefu",
    remaining: "Kilichobaki",
    elapsedSuffix: "{duration} zimepita",
    durationSuffix: "urefu {duration}",
    remainingSuffix: "{duration} zimebaki",
    showElapsed: "Onyesha muda uliopita, {duration}.",
    showDuration: "Onyesha urefu, {duration}.",
    showRemaining: "Onyesha muda uliobaki, {duration}.",
    toggleElapsed: "Badilisha kati ya muda uliopita na uliobaki.",
    toggleDuration: "Badilisha kati ya urefu na muda uliobaki.",
    position: "{current} kati ya {duration}",
    unknown: "Haijapakiwa, muda haujulikani.",
  },
  playback: { rate: "Kasi ya kucheza, {rate}" },
  volume: {
    mutedValue: "{percent}, sauti imezimwa",
    muted: "Sauti imezimwa",
    label: "Sauti",
    value: "Sauti {value}",
  },
  status: {
    captionsOn: "Manukuu yamewashwa",
    captionsOff: "Manukuu yamezimwa",
    paused: "Imesitishwa",
    playing: "Inacheza",
    fullscreen: "Skrini nzima",
    pip: "Picha ndani ya picha",
    exitPip: "Toka kwenye picha ndani ya picha",
    seekedTo: "Imesogezwa hadi {time}",
  },
  container: { label: "Kicheza video" },
  errors: {
    aborted: "Ulisimamisha uchezaji kabla haujaisha.",
    network: "Haikuweza kupakiwa kwa sababu ya tatizo la mtandao au seva.",
    decode:
      "Haikuweza kuchezwa. Huenda imeharibika, au kivinjari chako hakiitumii aina hii ya faili.",
    source:
      "Haikuweza kupakiwa. Huenda haipatikani, au kivinjari chako hakiitumii aina hii ya faili.",
    encrypted: "Haikuweza kuchezwa kwa sababu haikuweza kufunguliwa.",
    unplayable: "Kicheza hiki hakiitumii faili hili.",
    title: "Kuna tatizo.",
    unexpected: "Hitilafu isiyotarajiwa imetokea.",
  },
  common: { empty: "", ok: "Sawa" },
  menu: {
    settings: "Mipangilio",
    quality: "Ubora",
    audio: "Sauti",
    default: "Chaguo-msingi",
    speed: "Kasi",
    captions: "Manukuu",
    playbackRate: "Kasi ya kucheza",
    customRate: "Maalum, {rate}",
    back: "Rudi",
    off: "Imezimwa",
    auto: "Otomatiki",
    autoWithLabel: "Otomatiki ({label})",
    subtitles: "Manukuu",
  },
} satisfies Translations;

registerI18n("sw", sw);
