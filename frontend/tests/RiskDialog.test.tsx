import { beforeEach, expect, it, jest } from '@jest/globals';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { RiskDialog } from '../src/components/RiskDialog';

beforeEach(() => {
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute('open', '');
  };
  HTMLDialogElement.prototype.close = function () {
    this.removeAttribute('open');
  };
});
it('contains keyboard focus, requires exact target and restores focus on Escape without acting', async () => {
  const confirm = jest.fn();
  function Example() {
    const [open, setOpen] = useState(false);
    return (
      <>
        <button onClick={() => setOpen(true)}>Review</button>
        {open && (
          <RiskDialog
            title="Transfer ownership"
            target="Member A"
            impact="This grants full workspace control."
            typedTarget
            evidenceValid
            pending={false}
            onCancel={() => setOpen(false)}
            onConfirm={confirm}
          />
        )}
      </>
    );
  }
  render(<Example />);
  const keyboard = userEvent.setup();
  await keyboard.tab();
  await keyboard.keyboard('{Enter}');
  expect(screen.getByRole('button', { name: 'Cancel' })).toHaveFocus();
  expect(screen.getByRole('dialog')).toHaveAccessibleName('Transfer ownership');
  expect(screen.getByRole('dialog')).toHaveAccessibleDescription(
    'Warning: This grants full workspace control.',
  );
  expect(screen.getByRole('button', { name: 'Confirm' })).toBeDisabled();
  await keyboard.tab();
  expect(screen.getByLabelText('Type Member A to confirm')).toHaveFocus();
  await keyboard.type(screen.getByRole('textbox'), 'Member A');
  await keyboard.tab();
  await keyboard.tab();
  expect(screen.getByRole('button', { name: 'Confirm' })).toHaveFocus();
  await keyboard.tab();
  expect(screen.getByRole('textbox')).toHaveFocus();
  await keyboard.keyboard('{Shift>}{Tab}{/Shift}');
  expect(screen.getByRole('button', { name: 'Confirm' })).toHaveFocus();
  await keyboard.keyboard('{Escape}');
  expect(confirm).not.toHaveBeenCalled();
  expect(screen.getByRole('button', { name: 'Review' })).toHaveFocus();
});
it('blocks invalid evidence and pending actions; a changed target resets proof', async () => {
  const confirm = jest.fn(),
    cancel = jest.fn();
  const props = {
    title: 'Review',
    target: 'A',
    impact: 'Control changes.',
    typedTarget: true,
    evidenceValid: false,
    pending: false,
    onCancel: cancel,
    onConfirm: confirm,
  };
  const view = render(<RiskDialog {...props} />);
  await userEvent.type(screen.getByRole('textbox'), 'A');
  expect(screen.getByRole('button', { name: 'Confirm' })).toBeDisabled();
  view.rerender(<RiskDialog {...props} evidenceValid />);
  await userEvent.click(screen.getByRole('button', { name: 'Confirm' }));
  expect(confirm).toHaveBeenCalledTimes(1);
  view.rerender(<RiskDialog {...props} evidenceValid target="B" />);
  expect(screen.getByRole('textbox')).toHaveValue('');
  view.rerender(<RiskDialog {...props} evidenceValid pending />);
  await userEvent.keyboard('{Escape}');
  expect(cancel).not.toHaveBeenCalled();
});
