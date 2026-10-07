import { useId, useRef, useEffect, type ReactNode } from 'react';
import { ApiError, type FieldError } from '../api/client';

const messages = {
  required: 'This field is required.',
  invalid: 'Enter a valid value.',
  out_of_range: 'Enter a value within the allowed range.',
};
export function Field({
  label,
  hint,
  error,
  children,
}: {
  label: string;
  hint?: string;
  error?: FieldError['code'] | undefined;
  children: (attributes: {
    id: string;
    'aria-describedby': string | undefined;
    'aria-invalid': boolean;
  }) => ReactNode;
}) {
  const id = useId();
  return (
    <div className="form-field">
      <label htmlFor={id}>{label}</label>
      {hint && <p id={`${id}-hint`}>{hint}</p>}
      {children({
        id,
        'aria-describedby':
          [hint && `${id}-hint`, error && `${id}-error`]
            .filter(Boolean)
            .join(' ') || undefined,
        'aria-invalid': !!error,
      })}
      {error && <p id={`${id}-error`}>Error: {messages[error]}</p>}
    </div>
  );
}
// Only locally defined text is rendered: arbitrary Error.message, provider text,
// submitted values, and even server-supplied request IDs never enter summaries.
export function ProblemSummary({ error }: { error: unknown }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (error) ref.current?.focus();
  }, [error]);
  if (!error) return null;
  const status = error instanceof ApiError ? error.status : 0;
  const text =
    status === 409
      ? 'The information changed or the last owner must be retained. Your entries are preserved. Reload current data before reviewing and trying again.'
      : status === 403 || status === 404
        ? 'Access is no longer available. Refresh workspace access to continue.'
        : status === 400 || status === 422
          ? 'Check the form fields and try again.'
          : 'The request could not be confirmed. Check current data before trying again.';
  return (
    <div className="problem" role="alert" tabIndex={-1} ref={ref}>
      <strong>Unable to complete request</strong>
      <p>{text}</p>
    </div>
  );
}
