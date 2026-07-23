export type AppearanceSolidTheme =
  'vermilion' | 'amber' | 'bamboo' | 'azure' | 'indigo' | 'purple' | 'peach' | 'ink';

export const solidThemeOptions: Array<{
  label: string;
  value: AppearanceSolidTheme;
  color: string;
}> = [
  { label: '朱砂', value: 'vermilion', color: '#c83c23' },
  { label: '藤黄', value: 'amber', color: '#d99a2b' },
  { label: '竹青', value: 'bamboo', color: '#3b7a57' },
  { label: '天青', value: 'azure', color: '#2f7f98' },
  { label: '靛蓝', value: 'indigo', color: '#244b8a' },
  { label: '黛紫', value: 'purple', color: '#5d3a7e' },
  { label: '桃夭', value: 'peach', color: '#d65a78' },
  { label: '玄色', value: 'ink', color: '#2b2b30' },
];

export function isSolidTheme(value: string): value is AppearanceSolidTheme {
  return solidThemeOptions.some((option) => option.value === value);
}
