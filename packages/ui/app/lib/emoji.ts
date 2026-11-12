/**
 * A curated emoji set, held here rather than pulled from a library.
 *
 * Every emoji package worth using ships the full Unicode set with names,
 * keywords and often sprite images — hundreds of kilobytes to several
 * megabytes, downloaded by everybody who opens the application, to support a
 * picker most people use for about six symbols.
 *
 * This is the set people actually reach for, plus enough breadth to search
 * through, at no download cost: the glyphs come from the reader's own system
 * font, so there is nothing to fetch and nothing to keep up to date.
 *
 * The reaction field accepts any emoji, so somebody whose keyboard has a
 * picker of its own is not limited to this list.
 */

export interface EmojiGroup {
  name: string;
  /** A representative emoji for the group's tab. */
  icon: string;
  emoji: EmojiEntry[];
}

export interface EmojiEntry {
  /** The emoji itself, which is what gets sent. */
  value: string;
  /** Words that should find it, lowercase. */
  keywords: string[];
}

function entry(value: string, ...keywords: string[]): EmojiEntry {
  return { value, keywords };
}

/**
 * The six that carry most reactions, offered without opening the picker.
 *
 * Chosen for coverage of what a reaction usually means — agreement, thanks,
 * amusement, sympathy, celebration, acknowledgement — rather than by
 * popularity, so the row is useful in a work conversation.
 */
export const quickReactions = ["👍", "❤️", "😂", "🎉", "🙏", "👀"];

export const emojiGroups: EmojiGroup[] = [
  {
    name: "Smileys",
    icon: "🙂",
    emoji: [
      entry("😀", "grin", "happy", "smile"),
      entry("😃", "happy", "smile", "joy"),
      entry("😄", "happy", "laugh", "smile"),
      entry("😁", "grin", "beam"),
      entry("😆", "laugh", "satisfied"),
      entry("😅", "sweat", "laugh", "relief"),
      entry("🤣", "rofl", "laugh", "rolling"),
      entry("😂", "joy", "laugh", "tears", "lol"),
      entry("🙂", "smile", "slight"),
      entry("🙃", "upside down", "silly"),
      entry("😉", "wink"),
      entry("😊", "blush", "smile", "happy"),
      entry("😇", "innocent", "angel", "halo"),
      entry("🥰", "love", "hearts", "adore"),
      entry("😍", "heart eyes", "love"),
      entry("😘", "kiss", "blow"),
      entry("😋", "yum", "tasty", "delicious"),
      entry("😜", "wink", "tongue", "playful"),
      entry("🤪", "zany", "silly", "goofy"),
      entry("🤔", "thinking", "hmm", "consider"),
      entry("🤫", "shush", "quiet", "secret"),
      entry("🤨", "eyebrow", "sceptical", "suspicious"),
      entry("😐", "neutral", "meh"),
      entry("😑", "expressionless", "blank"),
      entry("😶", "no mouth", "speechless"),
      entry("😏", "smirk", "smug"),
      entry("😒", "unamused", "unimpressed"),
      entry("🙄", "eye roll", "whatever"),
      entry("😬", "grimace", "awkward"),
      entry("🤥", "lying", "liar"),
      entry("😴", "sleep", "zzz", "tired"),
      entry("🤤", "drool"),
      entry("😪", "sleepy", "tired"),
      entry("😮", "surprised", "open mouth"),
      entry("😯", "hushed", "surprised"),
      entry("😲", "astonished", "shocked"),
      entry("😳", "flushed", "embarrassed"),
      entry("🥺", "pleading", "please", "puppy"),
      entry("😦", "frowning"),
      entry("😢", "cry", "sad", "tear"),
      entry("😭", "sob", "crying", "sad"),
      entry("😤", "triumph", "huff", "frustrated"),
      entry("😠", "angry", "cross"),
      entry("😡", "rage", "furious", "angry"),
      entry("🤬", "swearing", "cursing"),
      entry("😱", "scream", "fear", "shocked"),
      entry("😨", "fearful", "scared"),
      entry("😰", "anxious", "worried", "sweat"),
      entry("😥", "sad", "relieved", "disappointed"),
      entry("😓", "downcast", "sweat"),
      entry("🤗", "hug", "hugging"),
      entry("🤭", "hand over mouth", "oops", "giggle"),
      entry("😷", "mask", "ill", "sick"),
      entry("🤒", "thermometer", "ill", "sick"),
      entry("🤕", "bandage", "hurt", "injured"),
      entry("🥳", "party", "celebrate", "birthday"),
      entry("🥴", "woozy", "dizzy"),
      entry("😎", "sunglasses", "cool"),
      entry("🤓", "nerd", "geek", "glasses"),
      entry("🧐", "monocle", "scrutiny", "inspect"),
    ],
  },
  {
    name: "Gestures",
    icon: "👋",
    emoji: [
      entry("👍", "thumbs up", "yes", "agree", "ok", "good", "+1"),
      entry("👎", "thumbs down", "no", "disagree", "bad", "-1"),
      entry("👌", "ok", "perfect", "fine"),
      entry("🤌", "pinched", "italian"),
      entry("✌️", "victory", "peace"),
      entry("🤞", "fingers crossed", "hope", "luck"),
      entry("🤟", "love you"),
      entry("🤘", "rock", "horns"),
      entry("🤙", "call me", "shaka"),
      entry("👈", "point left"),
      entry("👉", "point right"),
      entry("👆", "point up", "this"),
      entry("👇", "point down"),
      entry("☝️", "point up", "one"),
      entry("✋", "raised hand", "stop", "high five"),
      entry("🤚", "raised back of hand"),
      entry("🖐️", "hand", "fingers splayed"),
      entry("🖖", "vulcan", "spock"),
      entry("👋", "wave", "hello", "hi", "bye"),
      entry("🤝", "handshake", "deal", "agree"),
      entry("🙏", "please", "thanks", "pray", "asante"),
      entry("👏", "clap", "applause", "well done"),
      entry("🙌", "raised hands", "celebrate", "hooray"),
      entry("💪", "muscle", "strong", "flex"),
      entry("🫶", "heart hands", "love"),
      entry("✍️", "writing", "note"),
      entry("👀", "eyes", "looking", "watching"),
      entry("🫡", "salute", "yes sir", "acknowledged"),
      entry("🤦", "facepalm", "disbelief"),
      entry("🤷", "shrug", "dunno", "who knows"),
    ],
  },
  {
    name: "Hearts",
    icon: "❤️",
    emoji: [
      entry("❤️", "red heart", "love"),
      entry("🧡", "orange heart"),
      entry("💛", "yellow heart"),
      entry("💚", "green heart"),
      entry("💙", "blue heart"),
      entry("💜", "purple heart"),
      entry("🖤", "black heart"),
      entry("🤍", "white heart"),
      entry("🤎", "brown heart"),
      entry("💔", "broken heart"),
      entry("❣️", "heart exclamation"),
      entry("💕", "two hearts"),
      entry("💞", "revolving hearts"),
      entry("💓", "beating heart"),
      entry("💗", "growing heart"),
      entry("💖", "sparkling heart"),
      entry("💘", "heart with arrow", "cupid"),
      entry("💝", "heart with ribbon", "gift"),
    ],
  },
  {
    name: "Work",
    icon: "💻",
    emoji: [
      entry("💻", "laptop", "computer", "code"),
      entry("🖥️", "desktop", "computer", "monitor"),
      entry("⌨️", "keyboard", "typing"),
      entry("🖱️", "mouse"),
      entry("📱", "phone", "mobile"),
      entry("🐛", "bug", "issue", "defect"),
      entry("🔧", "wrench", "fix", "tool"),
      entry("🔨", "hammer", "build"),
      entry("⚙️", "gear", "settings", "config"),
      entry("🚀", "rocket", "ship", "launch", "deploy"),
      entry("🔥", "fire", "hot", "lit"),
      entry("✨", "sparkles", "new", "clean"),
      entry("⚡", "lightning", "fast", "zap"),
      entry("💡", "idea", "lightbulb", "suggestion"),
      entry("📌", "pin", "pinned"),
      entry("📎", "paperclip", "attachment"),
      entry("📅", "calendar", "date", "schedule"),
      entry("⏰", "alarm", "time", "reminder"),
      entry("⏳", "hourglass", "waiting", "pending"),
      entry("✅", "check", "done", "complete", "yes"),
      entry("❌", "cross", "no", "wrong", "fail"),
      entry("⚠️", "warning", "caution"),
      entry("🚧", "construction", "work in progress", "wip"),
      entry("📝", "memo", "note", "write"),
      entry("📊", "chart", "graph", "data"),
      entry("📈", "chart up", "growth", "increase"),
      entry("📉", "chart down", "decrease", "loss"),
      entry("🔍", "search", "magnify", "find"),
      entry("🔒", "lock", "secure", "private"),
      entry("🔑", "key", "access", "password"),
      entry("💾", "save", "disk", "floppy"),
      entry("🗑️", "bin", "delete", "trash"),
      entry("♻️", "recycle", "refactor"),
      entry("🎯", "target", "goal", "aim"),
      entry("🏁", "finish", "flag", "done"),
    ],
  },
  {
    name: "Things",
    icon: "🎉",
    emoji: [
      entry("🎉", "party", "celebrate", "tada", "hooray"),
      entry("🎊", "confetti", "celebrate"),
      entry("🎈", "balloon", "party"),
      entry("🎁", "gift", "present"),
      entry("🏆", "trophy", "win", "award"),
      entry("🥇", "gold", "first", "winner"),
      entry("⭐", "star", "favourite"),
      entry("🌟", "glowing star", "shine"),
      entry("☕", "coffee", "kahawa", "break"),
      entry("🍵", "tea", "chai"),
      entry("🍕", "pizza", "food"),
      entry("🍔", "burger", "food"),
      entry("🍽️", "food", "meal", "eat"),
      entry("🎵", "music", "note"),
      entry("📷", "camera", "photo"),
      entry("🎥", "video", "film", "camera"),
      entry("📞", "phone", "call"),
      entry("✉️", "email", "mail", "message"),
      entry("🌍", "earth", "world", "africa", "globe"),
      entry("🇹🇿", "tanzania", "flag"),
      entry("☀️", "sun", "sunny", "jua"),
      entry("🌧️", "rain", "mvua"),
      entry("🌙", "moon", "night"),
      entry("💰", "money", "cash", "pesa"),
      entry("🏠", "house", "home"),
      entry("🚗", "car", "drive"),
      entry("✈️", "plane", "flight", "travel"),
      entry("💯", "hundred", "perfect", "score"),
      entry("🆗", "ok", "sawa"),
      entry("🔁", "repeat", "loop", "retry"),
    ],
  },
];

/**
 * Finds emoji by keyword.
 *
 * Prefix-matched rather than substring: typing "car" should find the car
 * before it finds "scared", which is what a search-as-you-type box needs to
 * feel like it is narrowing rather than shuffling.
 */
export function searchEmoji(query: string): EmojiEntry[] {
  const term = query.trim().toLowerCase();
  if (!term) return [];

  const exact: EmojiEntry[] = [];
  const partial: EmojiEntry[] = [];

  for (const group of emojiGroups) {
    for (const item of group.emoji) {
      if (item.keywords.some((keyword) => keyword === term)) {
        exact.push(item);
      } else if (item.keywords.some((keyword) => keyword.startsWith(term))) {
        partial.push(item);
      }
    }
  }

  return [...exact, ...partial];
}
