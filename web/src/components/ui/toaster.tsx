import { Toaster as HotToaster } from 'react-hot-toast'

const Toaster = () => {
  return (
    <HotToaster
      position="bottom-right"
      containerStyle={{
        zIndex: 99999,
      }}
      containerClassName="select-text"
      toastOptions={{
        duration: 5000,
        className: 'select-text',
        style: {
          background: 'var(--color-background)',
          color: 'var(--color-foreground)',
          border: '1px solid var(--color-border)',
          borderRadius: '0.5rem',
          padding: '1rem',
          boxShadow: '0 10px 15px -3px rgb(0 0 0 / 0.1), 0 4px 6px -4px rgb(0 0 0 / 0.1)',
          userSelect: 'text',
        },
        success: {
          style: {
            background: 'rgb(5 150 105)',
            color: 'white',
            border: '1px solid rgb(6 95 70 / 0.4)',
          },
        },
        error: {
          style: {
            background: 'var(--color-destructive)',
            color: 'var(--color-destructive-foreground)',
            border: '1px solid var(--color-destructive)',
          },
        },
      }}
    />
  )
}

export { Toaster }
