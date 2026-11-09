import { FileInput, Text } from '@mantine/core';
import { FileKey } from 'lucide-react';
import { useState } from 'react';

import { isUnchanged } from '../../domain/connectorSecrets';

/** Largest key file accepted; a Firebase service account key is about 2.5 KB. */
const MAX_KEY_FILE_BYTES = 64 * 1024;

interface SecretFileFieldProps {
  label: string;
  description?: string;
  required?: boolean;
  value: unknown;
  /** Whether the server holds a key the form never saw. */
  keepsStored: boolean;
  onChange: (value: string) => void;
}

/**
 * A credential that comes as a file, such as a Firebase service account key.
 *
 * The file is read in the browser and sent as text, like any other secret
 * field. Its contents are never shown: once stored, the server sends back only
 * the unchanged marker, and choosing no file keeps what is stored.
 */
export function SecretFileField({ label, description, required, value, keepsStored, onChange }: SecretFileFieldProps) {
  const [file, setFile] = useState<File | null>(null);
  const [error, setError] = useState<string | null>(null);
  const stored = keepsStored && isUnchanged(value);

  const choose = async (chosen: File | null) => {
    setError(null);
    setFile(chosen);
    if (!chosen) return;
    if (chosen.size > MAX_KEY_FILE_BYTES) {
      setError('That file is too large to be a key file.');
      return;
    }
    onChange(await chosen.text());
  };

  return (
    <div>
      <FileInput
        label={label}
        description={description}
        required={required && !keepsStored}
        accept="application/json,.json"
        placeholder={stored ? 'A key is stored. Choose a file to replace it.' : 'Choose the JSON key file'}
        leftSection={<FileKey size={16} />}
        value={file}
        onChange={(chosen) => void choose(chosen)}
        error={error}
        clearable
      />
      {file && !error && (
        <Text size="xs" c="dimmed" mt={4}>
          {file.name} is loaded. It is sent when you save and cannot be read back.
        </Text>
      )}
    </div>
  );
}
