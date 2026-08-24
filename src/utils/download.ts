export type DownloadBlobOptions = {
  filename: string;
  blob: Blob;
  revokeDelayMs?: number;
  withBom?: boolean;
};

export function downloadBlob({ filename, blob, revokeDelayMs = 1000, withBom = false }: DownloadBlobOptions) {
  const normalizedBlob = withBom
    ? new Blob(['\uFEFF', blob], { type: blob.type || 'application/octet-stream' })
    : blob;
  const url = window.URL.createObjectURL(normalizedBlob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  link.rel = 'noopener';
  link.style.display = 'none';
  document.body.appendChild(link);
  link.click();

  window.setTimeout(() => {
    window.URL.revokeObjectURL(url);
    link.remove();
  }, revokeDelayMs);
}
