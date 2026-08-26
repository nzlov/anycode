<template>
  <q-card class="prompt-edit-dialog app-content-dialog" aria-label="编辑追加提示">
    <q-card-section class="prompt-edit-dialog__header">
      <div class="text-subtitle1 text-weight-bold">编辑追加提示</div>
      <q-btn
        flat
        round
        dense
        class="app-icon-btn"
        icon="close"
        aria-label="关闭"
        :disable="saving"
        @click="emit('cancel')"
      >
        <q-tooltip>关闭</q-tooltip>
      </q-btn>
    </q-card-section>
    <q-separator />
    <q-card-section class="prompt-edit-dialog__body">
      <q-banner v-if="error" dense class="prompt-edit-dialog__error">
        <template #avatar>
          <q-icon name="error_outline" />
        </template>
        {{ error }}
      </q-banner>
      <q-input
        :model-value="body"
        outlined
        type="textarea"
        autogrow
        label="追加提示正文"
        :disable="saving"
        @update:model-value="emit('update:body', String($event ?? ''))"
      />
      <div
        v-if="target?.attachments.length || target?.annotations.length || target?.artifacts.length"
        class="prompt-edit-dialog__attachments"
      >
        <div class="text-caption text-muted">附件保持不变</div>
        <div class="append-history__attachments">
          <q-chip
            v-for="attachment in target.attachments"
            :key="attachment.id"
            dense
            square
            outline
            :icon="attachment.kind === 'annotation' ? 'rate_review' : 'attach_file'"
            color="primary"
            text-color="primary"
            :label="attachmentLabel(attachment)"
          />
          <q-chip
            v-for="annotation in target.annotations"
            :key="annotation.id"
            dense
            square
            outline
            clickable
            icon="rate_review"
            color="primary"
            text-color="primary"
            :label="`批注 · ${annotation.source}`"
            @click="emit('preview-annotation', annotation)"
          >
            <q-icon name="visibility" class="q-ml-xs" />
            <q-tooltip>预览原文件及批注位置</q-tooltip>
          </q-chip>
          <q-chip
            v-for="artifact in target.artifacts"
            :key="artifact.id"
            dense
            square
            outline
            icon="link"
            color="primary"
            text-color="primary"
            :label="artifact.logicalPath || artifact.filename"
          />
        </div>
      </div>
    </q-card-section>
    <q-separator />
    <q-card-actions align="right">
      <q-btn flat no-caps icon="close" label="取消" :disable="saving" @click="emit('cancel')" />
      <q-btn
        unelevated
        no-caps
        color="primary"
        icon="save"
        label="保存"
        :loading="saving"
        :disable="!canSave"
        @click="emit('save')"
      />
    </q-card-actions>
  </q-card>
</template>

<script setup lang="ts">
import type { PromptAppend } from '@/services/sessions';
import type { SessionAttachment } from '@/services/sessions';
import type { PreviewAnnotationAttachment } from '@/services/previewAnnotations';

defineProps<{
  target: PromptAppend | null;
  body: string;
  saving?: boolean;
  error?: string;
  canSave?: boolean;
}>();

const emit = defineEmits<{
  'update:body': [value: string];
  cancel: [];
  save: [];
  'preview-annotation': [annotation: PreviewAnnotationAttachment];
}>();

function attachmentLabel(attachment: SessionAttachment) {
  if (attachment.kind !== 'annotation') return attachment.filename;
  const source = attachment.filename.replace(/^批注-/, '').replace(/\.md$/i, '');
  return `旧版批注 · ${source || '无法回放位置'}`;
}
</script>
