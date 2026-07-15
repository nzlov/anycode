export const workflowValueTypeOptions = ['string', 'number', 'boolean', 'object', 'array', 'any'];

const systemOutputFieldKeys = new Set(['approval.approved', 'merge.status', 'merge.failureCode', 'merge.failureReason']);

export function systemOutputFields(type, hasMerge) {
  const fields = [];
  if (type === 'merge' || hasMerge) {
    fields.push(
      {
        key: 'merge.status',
        description: '合并执行状态',
        valueType: 'string',
      },
      {
        key: 'merge.failureCode',
        description: '合并未完成时的失败代码',
        valueType: 'string',
      },
      {
        key: 'merge.failureReason',
        description: '合并未完成时的失败原因',
        valueType: 'string',
      },
    );
  }
  return fields;
}

export function completeOutputFields(fields, required) {
  const requiredKeys = new Set(required.map((field) => field.key));
  const normalized = fields
    .map(normalizeOutputField)
    .filter((field) => field.key)
    .filter((field) => requiredKeys.has(field.key) || !systemOutputFieldKeys.has(field.key));
  required.forEach((requiredField) => {
    const index = normalized.findIndex((field) => field.key === requiredField.key);
    if (index >= 0) {
      normalized.splice(index, 1, { ...requiredField });
      return;
    }
    normalized.push({ ...requiredField });
  });
  return normalized;
}

function normalizeOutputField(field) {
  const valueType = workflowValueTypeOptions.includes(String(field.valueType)) ? String(field.valueType) : 'string';
  return {
    key: String(field.key ?? '').trim(),
    description: String(field.description ?? '').trim(),
    valueType,
  };
}
