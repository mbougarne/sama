import { expect, it } from '@jest/globals';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { Field, ProblemSummary } from '../src/components/Form';
import { ApiError } from '../src/api/client';

it('labels fields, explains invalid submission and moves keyboard focus to a safe summary', async () => {
  function Form() {
    const [invalid, setInvalid] = useState(false);
    return (
      <form
        onSubmit={(event) => {
          event.preventDefault();
          setInvalid(true);
        }}
      >
        <ProblemSummary
          error={
            invalid ? new ApiError(422, 'invalid_request', 'SECRET') : null
          }
        />
        <Field
          label="Account name"
          hint="Use the account label."
          error={invalid ? 'required' : undefined}
        >
          {(attributes) => <input {...attributes} />}
        </Field>
        <button>Save</button>
      </form>
    );
  }
  render(<Form />);
  const keyboard = userEvent.setup();
  await keyboard.tab();
  expect(screen.getByLabelText('Account name')).toHaveFocus();
  await keyboard.tab();
  await keyboard.keyboard('{Enter}');
  expect(screen.getByRole('alert')).toHaveFocus();
  expect(screen.getByLabelText('Account name')).toHaveAccessibleDescription(
    'Use the account label. Error: This field is required.',
  );
  expect(screen.getByLabelText('Account name')).toHaveAttribute(
    'aria-invalid',
    'true',
  );
  expect(document.body).not.toHaveTextContent('SECRET');
});
it('escapes untrusted labels and ignores arbitrary errors including secrets', () => {
  const text = '<img src=x onerror=alert(1)>';
  render(
    <>
      <Field label={text}>{(props) => <input {...props} />}</Field>
      <ProblemSummary error={new Error('SECRET')} />
    </>,
  );
  expect(screen.getByLabelText(text)).toBeVisible();
  expect(document.querySelector('img')).toBeNull();
  expect(document.body).not.toHaveTextContent('SECRET');
});
