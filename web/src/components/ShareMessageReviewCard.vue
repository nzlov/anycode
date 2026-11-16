<template>
  <q-card flat bordered class="q-mb-md">
    <q-card-section>
      <div class="share-message-heading">
        <q-badge :color="message.status === 'pending' ? 'warning' : 'grey'">{{
          shareMessageStatus(message.status)
        }}</q-badge>
        <span class="text-caption share-id">分享 {{ message.shareId }}</span>
        <q-btn
          v-if="showSession"
          flat
          dense
          no-caps
          color="primary"
          icon="open_in_new"
          label="打开所属会话"
          :to="{ name: 'session-detail', params: { id: message.sessionId } }"
          @click="emit('openSession')"
        />
      </div>
      <div class="text-caption text-muted q-mt-sm">
        {{ new Date(message.createdAt).toLocaleString() }} ·
        {{ message.content.config.CodexModel || '默认模型' }} ·
        {{ message.content.config.ReasoningEffort || '默认强度' }} ·
        {{ message.content.config.PermissionMode
        }}{{ message.content.config.FastMode ? ' · Fast' : '' }}
      </div>
      <div class="share-body q-mt-sm">{{ message.content.body }}</div>
      <q-btn
        v-for="file in message.content.attachments"
        :key="file.id"
        flat
        dense
        icon="attach_file"
        :label="file.filename"
        @click="downloadReviewAttachment(message.id, file).catch((error) => emit('error', error))"
      />
    </q-card-section>
    <q-card-actions v-if="message.status === 'pending'" align="right">
      <q-btn
        flat
        color="negative"
        label="拒绝"
        :disable="!!busy"
        @click="emit('review', message.id, false)"
      />
      <q-btn
        unelevated
        color="primary"
        label="批准并发送"
        :loading="busy === message.id"
        :disable="!!busy"
        @click="emit('review', message.id, true)"
      />
    </q-card-actions>
  </q-card>
</template>

<script setup lang="ts">
import {
  downloadReviewAttachment,
  shareMessageStatus,
  type ShareMessage,
} from '@/services/sharing';

withDefaults(defineProps<{ message: ShareMessage; busy: string; showSession?: boolean }>(), {
  showSession: false,
});
const emit = defineEmits<{
  review: [id: string, approved: boolean];
  error: [error: unknown];
  openSession: [];
}>();
</script>

<style scoped>
.share-message-heading {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.share-id,
.share-body {
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}
</style>
