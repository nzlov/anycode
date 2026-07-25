import { computed, ref } from 'vue';

export type SessionThinkingPhraseStyle =
  'normal' | 'chuunibyou' | 'crazy' | 'maid' | 'kaomoji' | 'funny_emoji';

export const sessionThinkingPhraseStyleOptions: Array<{
  label: string;
  value: SessionThinkingPhraseStyle;
}> = [
  { label: '普通', value: 'normal' },
  { label: '中二', value: 'chuunibyou' },
  { label: '疯狂', value: 'crazy' },
  { label: '女仆', value: 'maid' },
  { label: '颜文字', value: 'kaomoji' },
  { label: '滑稽 Emoji', value: 'funny_emoji' },
];

export interface SessionThinkingPhrasePreferences {
  enabled: boolean;
  style: SessionThinkingPhraseStyle;
}

export const sessionThinkingPhrasePreferencesStorageKey = 'anycode.session-thinking-phrases.v1';

const phrasesByStyle: Record<SessionThinkingPhraseStyle, readonly string[]> = {
  normal: [
    '我想想……',
    '让我理一理……',
    '正在梳理思路……',
    '先从关键点入手……',
    '我来仔细看看……',
    '让我换个角度……',
    '正在检查细节……',
    '稍等，我推演一下……',
    '我在确认下一步……',
    '先把线索串起来……',
    '让我核对一下……',
    '正在分析上下文……',
    '我来拆解这个问题……',
    '嗯，再想深一点……',
    '让我权衡一下……',
    '正在寻找更简洁的办法……',
    '先确认有没有遗漏……',
    '我在整理答案……',
    '让我验证一下判断……',
    '快有眉目了……',
  ],
  chuunibyou: [
    '命运的齿轮开始转动……',
    '沉睡的逻辑，苏醒吧……',
    '让我解开这道世界线……',
    '真相正在深渊中回响……',
    '封印的答案即将显现……',
    '以代码之名，洞察一切……',
    '黑暗中浮现了一丝线索……',
    '这就是因果的交汇点吗……',
    '我的直觉在发出警告……',
    '让思维穿越迷雾……',
    '禁忌的推演开始了……',
    '万象皆有其规则……',
    '看来必须认真起来了……',
    '另一条世界线正在展开……',
    '逻辑之眼已经睁开……',
    '让隐藏的脉络现形吧……',
    '答案就在次元的彼端……',
    '哼，事情开始有趣了……',
    '这股违和感究竟是……',
    '最终解即将降临……',
  ],
  crazy: [
    '哈哈哈，线索全都连起来了……',
    '等等，这也太有意思了……',
    '再快一点，思路要追上来了……',
    '好多可能性，一起算吧……',
    '不对不对，再翻个面看看……',
    '啊哈，我抓到那个细节了……',
    '让所有假设同时起跑……',
    '脑内风暴正在全速旋转……',
    '这条路不通？那就开一条……',
    '越复杂越让人兴奋……',
    '再大胆一点，再验证一次……',
    '逻辑正在疯狂增殖……',
    '停不下来，马上就有答案了……',
    '先把整个问题摇匀……',
    '每个细节都在尖叫……',
    '来吧，把边界全部推开……',
    '我闻到答案的味道了……',
    '思路正在进行极限漂移……',
    '太妙了，再深挖一层……',
    '轰——答案快要成形了……',
  ],
  maid: [
    '请稍等，正在为主人整理思路……',
    '主人，请让我仔细确认一下……',
    '正在为主人核对细节……',
    '请把这一步交给我吧……',
    '我会把线索整理得整整齐齐……',
    '稍等片刻，马上为您呈上答案……',
    '让我为主人换个角度看看……',
    '正在认真检查有没有遗漏……',
    '主人的问题，我一定会好好解决……',
    '请放心，我正在梳理上下文……',
    '我来把复杂的部分拆开吧……',
    '正在为主人寻找更简洁的办法……',
    '嗯……让我再核对一次……',
    '很快就好，请主人稍候……',
    '我已经抓住关键线索了……',
    '接下来也请交给我处理吧……',
    '正在把答案打磨得更清楚……',
    '让我为主人验证这个判断……',
    '请稍等，思路已经快整理好了……',
    '能为主人思考是我的荣幸……',
  ],
  kaomoji: [
    '我想想……(・_・;)',
    '正在整理思路……(｡•̀ᴗ-)✧',
    '让我仔细看看……( •̀ ω •́ )✧',
    '先理清关键点……(￣▽￣)ゞ',
    '等等，好像有线索……(⊙_⊙)',
    '再换个角度……( ˘•ω•˘ )',
    '正在核对细节……( ..)φ',
    '让我推演一下……(￣～￣;)',
    '思路正在成形……(ง •̀_•́)ง',
    '先确认没有遗漏……(｀・ω・´)',
    '我来拆解问题……(๑•̀ㅂ•́)و✧',
    '正在分析上下文……(。-`ω´-)',
    '嗯，再深入一点……(￣ω￣;)',
    '让我验证这个判断……( •̀ㅁ•́;)',
    '线索快串起来了……(☆▽☆)',
    '正在寻找简单办法……(｡･ω･｡)',
    '稍等，马上就好……ヾ(•ω•`)o',
    '我再核对一次……(。・_・。)',
    '答案越来越清楚了……(ﾉ◕ヮ◕)ﾉ',
    '快有眉目了……٩(ˊᗜˋ*)و',
  ],
  funny_emoji: [
    '我想想……🤔',
    '脑子开始转了……🧠⚙️',
    '先让我盘一盘……🧐',
    '线索正在排队……🧩',
    '等等，好像有戏……👀',
    '换个姿势再想想……🙃',
    '细节一个都别跑……🔍',
    '正在疯狂计算……🧮💨',
    '这题有点东西……😏',
    '思路正在加载……⏳',
    '让我大胆假设一下……🚀',
    '再小心验证一下……🧪',
    '脑内会议正在召开……🗣️',
    '先把问题切成小块……🍰',
    '答案正在赶来的路上……🏃💨',
    '差一点就抓住了……🫴',
    '大脑已切换搞笑频道……🤪',
    '不慌，先喝口空气……🥤',
    '逻辑开始跳舞了……💃',
    '马上揭晓……🥁',
  ],
};

const storedPreferences = ref<SessionThinkingPhrasePreferences>({
  enabled: false,
  style: 'normal',
});
let initialized = false;

export function useSessionThinkingPhrases() {
  if (!initialized) {
    storedPreferences.value = readSessionThinkingPhrasePreferences();
    initialized = true;
  }

  const thinkingPhrasesEnabled = computed({
    get: () => storedPreferences.value.enabled,
    set: (enabled: boolean) =>
      storeSessionThinkingPhrasePreferences({
        ...storedPreferences.value,
        enabled,
      }),
  });
  const thinkingPhraseStyle = computed({
    get: () => storedPreferences.value.style,
    set: (style: SessionThinkingPhraseStyle) =>
      storeSessionThinkingPhrasePreferences({ ...storedPreferences.value, style }),
  });
  const thinkingPhrases = computed(() => phrasesByStyle[storedPreferences.value.style]);

  return { thinkingPhrasesEnabled, thinkingPhraseStyle, thinkingPhrases };
}

export function readSessionThinkingPhrasePreferences(): SessionThinkingPhrasePreferences {
  const fallback: SessionThinkingPhrasePreferences = { enabled: false, style: 'normal' };
  if (typeof window === 'undefined') return fallback;
  try {
    const raw = window.localStorage.getItem(sessionThinkingPhrasePreferencesStorageKey);
    if (!raw) return fallback;
    const stored = JSON.parse(raw) as unknown;
    if (!stored || typeof stored !== 'object' || Array.isArray(stored)) return fallback;
    const record = stored as Record<string, unknown>;
    return {
      enabled: record.enabled === true,
      style: isSessionThinkingPhraseStyle(record.style) ? record.style : 'normal',
    };
  } catch {
    return fallback;
  }
}

function storeSessionThinkingPhrasePreferences(preferences: SessionThinkingPhrasePreferences) {
  storedPreferences.value = preferences;
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(
      sessionThinkingPhrasePreferencesStorageKey,
      JSON.stringify(preferences),
    );
  } catch {
    // The selected preferences remain active when browser storage is unavailable.
  }
}

function isSessionThinkingPhraseStyle(value: unknown): value is SessionThinkingPhraseStyle {
  return (
    value === 'normal' ||
    value === 'chuunibyou' ||
    value === 'crazy' ||
    value === 'maid' ||
    value === 'kaomoji' ||
    value === 'funny_emoji'
  );
}
