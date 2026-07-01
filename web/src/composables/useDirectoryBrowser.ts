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

  const directoryTree = computed(() => entries.value.map(directoryEntryToTreeNode));

  async function loadDirectory(path = rootPath) {
    loading.value = true;
    try {
      const page = await browseDirectory(path);
      currentPath.value = page.path;
      entries.value = page.entries;
    } finally {
      loading.value = false;
    }
  }

  return {
    currentPath,
    directoryTree,
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
