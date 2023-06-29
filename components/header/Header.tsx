import styles from "./header.module.css";
import cn from "classnames";

interface IHeaderProps {
  isAuthorization?: boolean;
  className?: string;
}

const Header = ({
  isAuthorization = false,
  className,
}: IHeaderProps): JSX.Element => {
  const content = isAuthorization ? <>BLOB</> : <div>auth</div>;

  return <header className={cn(styles.header, className)}>{content}</header>;
};

export default Header;
