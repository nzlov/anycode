import { computed, ref } from 'vue';

import { browseDirectory, type DirectoryEntry } from '@/services/projects';

export interface DirectoryTreeNode {
  label: string;
  path: string;
  icon: string;
  selectable: boolean;
  children?: DirectoryTreeNode[];
}

const rootPath = '/';

export function useDirectoryBrowser() {
  const entries = ref<DirectoryEntry[]>([]);
  const loading = ref(false);
  const currentPath = ref(rootPath);
  const parentPath = ref('');
  const error = ref('');

  const directoryTree = computed(() => entries.value.map(directoryEntryToTreeNode));

  async function loadDirectory(path = rootPath) {
    loading.value = true;
    error.value = '';
    try {
      const page = await browseDirectory(path);
      currentPath.value = page.path;
      parentPath.value = page.parent;
      entries.value = page.entries;
    } catch (err) {
      error.value = err instanceof Error ? err.message : '目录读取失败';
      entries.value = [];
      throw err;
    } finally {
      loading.value = false;
    }
  }

  return {
    currentPath,
    parentPath,
    entries,
    directoryTree,
    error,
    loading,
    loadDirectory,
  };
}

function directoryEntryToTreeNode(entry: DirectoryEntry): DirectoryTreeNode {
  const node: DirectoryTreeNode = {
    label: entry.name,
    path: entry.path,
    icon: entry.isGit ? 'folder_open' : 'folder',
    selectable: entry.isDir && entry.canRead,
  };
  if (entry.children) {
    node.children = entry.children.map(directoryEntryToTreeNode);
  }
  return node;
}
