import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react';
import { type FormField } from '../components/FormBuilder';
import { evaluateFormCondition, evaluateFormExpression } from '../domain/formExpression';

type FormValues = Record<string, unknown>;
type FormErrors = Record<string, string | null>;
type ExternalOptions = Record<string, Array<{ value: string; label: string }>>;

function evaluateExpression(expression: string, values: FormValues, variables: FormValues): boolean {
  return evaluateFormCondition(expression, { data: values, vars: variables });
}

function resolveDefaultValue(field: FormField, variables: FormValues): unknown {
  let value = variables[field.id];

  if (
    value === undefined &&
    typeof field.defaultValue === 'string' &&
    field.defaultValue.startsWith('{{') &&
    field.defaultValue.endsWith('}}')
  ) {
    const expression = field.defaultValue.slice(2, -2);
    try {
      value = evaluateFormExpression(expression, { data: {}, vars: variables });
    } catch (error) {
      console.warn(
        `A default-value expression was refused and the field is left empty: ${expression}`,
        error instanceof Error ? error.message : error,
      );
    }
  }

  if (value !== undefined && value !== null) {
    return value;
  }

  if (field.defaultValue !== undefined) {
    return field.defaultValue;
  }

  if (field.type === 'boolean') {
    return false;
  }

  if (field.type === 'number') {
    return 0;
  }

  return '';
}

function buildInitialValues(fields: FormField[], variables: FormValues): FormValues {
  return fields.reduce((accumulator, field) => {
    accumulator[field.id] = resolveDefaultValue(field, variables);
    return accumulator;
  }, {} as FormValues);
}

function normalizeOptions(data: unknown): Array<{ value: string; label: string }> {
  if (!Array.isArray(data)) {
    return [];
  }

  return data.map((item) => (typeof item === 'string' ? { value: item, label: item } : item));
}

export function useTaskForm(fields: FormField[], variables: FormValues, onSubmit: (values: FormValues) => void) {
  const [values, setValues] = useState<FormValues>(() => buildInitialValues(fields, variables));
  const [errors, setErrors] = useState<FormErrors>({});
  const [touched, setTouched] = useState<Record<string, boolean>>({});
  const [externalData, setExternalData] = useState<ExternalOptions>({});

  // Reset the form during render rather than in an effect.
  //
  // As an effect keyed on [fields, variables] this ran after every render in
  // which either identity changed — and callers routinely pass a fresh array
  // (`definition.fields || []`), so it re-ran constantly and wiped whatever the
  // user had typed. React's documented "adjusting state when a prop changes"
  // pattern compares against the previous inputs instead, so the reset happens
  // exactly once per genuine change and before anything is painted.
  const [previousInputs, setPreviousInputs] = useState({ fields, variables });
  if (previousInputs.fields !== fields || previousInputs.variables !== variables) {
    setPreviousInputs({ fields, variables });
    setValues(buildInitialValues(fields, variables));
    setErrors({});
    setTouched({});
  }

  useEffect(() => {
    const endpointFields = fields.filter(
      (field) => field.type === 'select' && field.dataSource?.type === 'endpoint' && field.dataSource.endpointUrl,
    );

    if (endpointFields.length === 0) {
      return;
    }

    const abortControllers: AbortController[] = [];

    endpointFields.forEach((field) => {
      const controller = new AbortController();
      abortControllers.push(controller);

      fetch(field.dataSource!.endpointUrl!, { signal: controller.signal })
        .then((response) => response.json())
        .then((data) => {
          setExternalData((previousData) => ({
            ...previousData,
            [field.id]: normalizeOptions(data),
          }));
        })
        .catch((error: unknown) => {
          if (error instanceof Error && error.name === 'AbortError') {
            return;
          }

          console.error(`Error fetching data for field ${field.id}:`, error);
        });
    });

    return () => {
      abortControllers.forEach((controller) => controller.abort());
    };
  }, [fields]);

  const validateField = useCallback(
    (field: FormField, currentValues: FormValues): string | null => {
      if (evaluateExpression(field.logic?.hiddenIf || '', currentValues, variables)) {
        return null;
      }

      const value = currentValues[field.id];

      if (field.required && !value && value !== 0 && value !== false) {
        return 'This field is required';
      }

      if (field.validation?.pattern && value && !new RegExp(field.validation.pattern).test(String(value))) {
        return field.validation.message || 'Invalid format';
      }

      // `customJs` is named for what it used to be, and it is no longer that.
      // It came from a deployed definition and was executed with new Function,
      // which is how a modeller ran code in an approver's browser. It now goes
      // through the same bounded evaluator as the rest of the form logic, so a
      // rule that is a comparison keeps working and a rule that is a program
      // does not.
      //
      // `value` is exposed under `data.value` rather than as a bare name,
      // because the evaluator reads from `data` and `vars` and nothing else.
      if (field.validation?.customJs && value) {
        const scope = { data: { ...currentValues, value }, vars: variables };
        try {
          const isValid = evaluateFormExpression(field.validation.customJs, scope);
          if (isValid !== true) {
            return typeof isValid === 'string' ? isValid : (field.validation.message || 'Validation failed');
          }
        } catch (error) {
          console.warn(
            `A custom validation rule was refused, so the field is not being checked by it: ${field.validation.customJs}`,
            error instanceof Error ? error.message : error,
          );
        }
      }

      return null;
    },
    [variables],
  );

  const handleSubmit = useCallback(
    (event: FormEvent) => {
      event.preventDefault();

      const nextErrors: FormErrors = {};
      fields.forEach((field) => {
        const error = validateField(field, values);
        if (error) {
          nextErrors[field.id] = error;
        }
      });

      if (Object.keys(nextErrors).length > 0) {
        setErrors(nextErrors);
        setTouched(fields.reduce((accumulator, field) => ({ ...accumulator, [field.id]: true }), {}));
        return;
      }

      onSubmit(values);
    },
    [fields, onSubmit, validateField, values],
  );

  const handleChange = useCallback(
    (id: string, value: unknown) => {
      const nextValues = { ...values, [id]: value };
      setValues(nextValues);

      const field = fields.find((currentField) => currentField.id === id);
      if (field && touched[id]) {
        const error = validateField(field, nextValues);
        setErrors((previousErrors) => ({ ...previousErrors, [id]: error }));
      }
    },
    [fields, touched, validateField, values],
  );

  const handleBlur = useCallback(
    (id: string) => {
      setTouched((previousTouched) => ({ ...previousTouched, [id]: true }));
      const field = fields.find((currentField) => currentField.id === id);
      if (!field) {
        return;
      }

      const error = validateField(field, values);
      setErrors((previousErrors) => ({ ...previousErrors, [id]: error }));
    },
    [fields, validateField, values],
  );

  const visibleFields = useMemo(
    () => fields.filter((field) => !evaluateExpression(field.logic?.hiddenIf || '', values, variables)),
    [fields, values, variables],
  );

  const isDisabled = useCallback(
    (field: FormField) => evaluateExpression(field.logic?.disabledIf || '', values, variables),
    [values, variables],
  );

  const getSelectOptions = useCallback(
    (field: FormField) => {
      if (field.dataSource?.type === 'variable') {
        return normalizeOptions(variables[field.dataSource.variableKey || '']);
      }

      if (field.dataSource?.type === 'endpoint') {
        return externalData[field.id] || [];
      }

      return field.options || [];
    },
    [externalData, variables],
  );

  return {
    values,
    errors,
    visibleFields,
    handleSubmit,
    handleChange,
    handleBlur,
    isDisabled,
    getSelectOptions,
  };
}