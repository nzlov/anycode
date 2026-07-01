<template>
  <q-dialog :model-value="modelValue" @update:model-value="emitModel">
    <q-card class="directory-dialog">
      <q-card-section class="row items-center q-pb-sm">
        <div>
          <div class="text-subtitle1 text-weight-bold">选择项目目录</div>
          <div class="text-caption text-muted">目录树由后端权限范围决定</div>
        </div>
        <q-space />
        <q-btn v-close-popup flat round dense icon="close" aria-label="关闭" />
      </q-card-section>

      <q-separator />

      <q-card-section>
        <q-input v-model="filter" dense outlined label="过滤目录" clearable>
          <template #prepend>
            <q-icon name="search" />
          </template>
        </q-input>
        <q-tree
          v-model:selected="selected"
          :nodes="directoryTree"
          node-key="label"
          selected-color="primary"
          :filter="filter"
          class="q-mt-md"
        />
      </q-card-section>

      <q-card-actions align="right">
        <q-btn v-close-popup flat color="primary" label="取消" no-caps />
        <q-btn
          v-close-popup
          unelevated
          color="primary"
          icon="folder_open"
          label="使用目录"
          no-caps
        />
      </q-card-actions>
    </q-card>
  </q-dialog>
</template>

<script setup lang="ts">
import { ref } from 'vue';

import { directoryTree } from '@/mocks/workbench';

defineProps<{
  modelValue: boolean;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
}>();

const filter = ref('');
const selected = ref('anycode');

function emitModel(value: boolean) {
  emit('update:modelValue', value);
}
</script>
