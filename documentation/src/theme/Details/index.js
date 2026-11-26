import React from 'react';
import clsx from 'clsx';
import {Details as DetailsGeneric} from '@docusaurus/theme-common/Details';
import styles from './styles.module.css';

export default function Details({...props}) {
  return (
    <DetailsGeneric
      {...props}
      className={clsx('alert', styles.details, props.className)}
    />
  );
}
