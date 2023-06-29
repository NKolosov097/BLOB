import Header from "@/components/header/Header";
import styles from "./page.module.css";
import Authorization from "@/components/authorization/Authorization";

export const metadata = {
  title: "Blob | Register",
};

const Register = () => {
  return (
    <section style={{ height: "calc(100vh - 105px)" }}>
      <Header isAuthorization className="display: 'flex';" />
      <Authorization isRegister />
    </section>
  );
};

export default Register;
