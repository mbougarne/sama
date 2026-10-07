import { useEffect, useId, useRef, useState } from 'react';
import { Field, ProblemSummary } from './Form';

type Props = {
  title: string;
  target: string;
  impact: string;
  typedTarget?: boolean;
  evidenceValid: boolean;
  pending: boolean;
  error?: unknown;
  onCancel: () => void;
  onConfirm: () => void;
};
export function RiskDialog(props: Props) {
  // A changed review target discards its typed confirmation.
  return <Review key={props.target} {...props} />;
}
function Review({
  title,
  target,
  impact,
  typedTarget = false,
  evidenceValid,
  pending,
  error,
  onCancel,
  onConfirm,
}: Props) {
  const dialog = useRef<HTMLDialogElement>(null);
  const cancel = useRef<HTMLButtonElement>(null);
  const [typed, setTyped] = useState('');
  const id = useId();
  useEffect(() => {
    const opener = document.activeElement;
    const element = dialog.current!;
    element.showModal();
    cancel.current?.focus();
    return () => {
      element.close();
      if (opener instanceof HTMLElement && opener.isConnected) opener.focus();
    };
  }, []);
  const valid = evidenceValid && (!typedTarget || typed === target);
  return (
    <dialog
      ref={dialog}
      aria-labelledby={`${id}-title`}
      aria-describedby={`${id}-impact`}
      onCancel={(event) => {
        event.preventDefault();
        if (!pending) onCancel();
      }}
      onKeyDown={(event) => {
        if (event.key === 'Escape') {
          event.preventDefault();
          if (!pending) onCancel();
        }
        if (event.key !== 'Tab') return;
        const controls = Array.from(
          dialog.current!.querySelectorAll<HTMLElement>(
            'button:not(:disabled), input:not(:disabled), [tabindex="0"]',
          ),
        );
        const first = controls[0],
          last = controls.at(-1);
        if (
          event.shiftKey &&
          (document.activeElement === first ||
            !controls.includes(document.activeElement as HTMLElement))
        ) {
          event.preventDefault();
          last?.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first?.focus();
        }
      }}
    >
      <h2 id={`${id}-title`}>{title}</h2>
      <p>
        <strong>Target:</strong> {target}
      </p>
      <p id={`${id}-impact`}>
        <strong>Warning:</strong> {impact}
      </p>
      <ProblemSummary error={error} />
      <form
        onSubmit={(event) => {
          event.preventDefault();
          if (valid && !pending) onConfirm();
        }}
      >
        {typedTarget && (
          <Field label={`Type ${target} to confirm`}>
            {(attributes) => (
              <input
                {...attributes}
                value={typed}
                disabled={pending}
                autoComplete="off"
                onChange={(event) => setTyped(event.target.value)}
              />
            )}
          </Field>
        )}
        {!evidenceValid && (
          <p>
            Current authorization and target details must be loaded before
            confirmation.
          </p>
        )}
        <div className="dialog-actions">
          <button
            ref={cancel}
            type="button"
            disabled={pending}
            onClick={onCancel}
          >
            Cancel
          </button>
          <button disabled={pending || !valid}>
            {pending ? 'Confirming…' : 'Confirm'}
          </button>
        </div>
      </form>
    </dialog>
  );
}
