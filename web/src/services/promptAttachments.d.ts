interface FileTransferItem {
  kind: string;
  getAsFile(): File | null;
}

interface FileTransferLike {
  files?: ArrayLike<File> | null;
  items?: ArrayLike<FileTransferItem> | null;
}

export function filesFromTransfer(transfer: FileTransferLike | null): File[];
